package machine

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/distribution/reference"
	"gopkg.in/yaml.v3"
)

// Compose support is a constrained subset executed by the manager, never by
// `docker compose`: the file comes from the task checkout, so it is untrusted.
// Only image-based services on an internal, egress-free network are accepted.
// Builds, mounts, privileges, host networking and published ports would need an
// isolated Docker daemon and are rejected rather than weakened.

const (
	maxComposeFileBytes = 64 * 1024
	maxComposeServices  = 8
)

var (
	composeServiceName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
	composeEnvName     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	composeUser        = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}(:[A-Za-z0-9_.-]{1,64})?$`)
	composeVariable    = regexp.MustCompile(`\$\$|\$\{([A-Za-z_][A-Za-z0-9_]*)(?:(:?-)([^}]*))?\}|\$([A-Za-z_][A-Za-z0-9_]*)`)
	composeTopLevel    = []string{"services", "name", "version"}
	// ports, expose and restart are accepted for compatibility but never
	// applied: services are reached by name and live only for one check.
	composeServiceKeys = []string{"image", "environment", "command", "healthcheck", "depends_on", "ports", "expose", "restart", "user", "working_dir"}
)

// ComposeError is a rejected or unusable Compose definition. It is the
// project's configuration problem, so retrying cannot fix it.
type ComposeError struct{ Reason string }

func (e *ComposeError) Error() string { return "compose: " + e.Reason }

func composeErr(format string, args ...any) error {
	return &ComposeError{Reason: fmt.Sprintf(format, args...)}
}

type ComposeHealth struct {
	Test        []string
	Interval    time.Duration
	Timeout     time.Duration
	StartPeriod time.Duration
	Retries     int
}

type ComposeService struct {
	Name       string
	Image      string
	Env        []string
	Command    []string
	User       string
	WorkingDir string
	Health     *ComposeHealth
	DependsOn  []string
}

// ParseCompose validates a Compose file against the supported subset and
// returns its services in dependency order. allowedImages lists repository
// names or prefixes (for example "docker.io/library") the operator permits.
func ParseCompose(data []byte, allowedImages []string) ([]ComposeService, error) {
	if len(data) > maxComposeFileBytes {
		return nil, composeErr("file is larger than 64 KiB")
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, composeErr("invalid YAML")
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, composeErr("expected a mapping at the top level")
	}
	if err := rejectAliases(&document); err != nil {
		return nil, err
	}
	root := document.Content[0]
	var servicesNode *yaml.Node
	for i := 0; i < len(root.Content); i += 2 {
		key := root.Content[i].Value
		if !slices.Contains(composeTopLevel, key) {
			return nil, composeErr("top-level %q is not supported", key)
		}
		if key == "services" {
			servicesNode = root.Content[i+1]
		}
	}
	if servicesNode == nil || servicesNode.Kind != yaml.MappingNode || len(servicesNode.Content) == 0 {
		return nil, composeErr("no services are defined")
	}
	if len(servicesNode.Content)/2 > maxComposeServices {
		return nil, composeErr("at most %d services are supported", maxComposeServices)
	}
	services := map[string]ComposeService{}
	order := []string{}
	for i := 0; i < len(servicesNode.Content); i += 2 {
		name := servicesNode.Content[i].Value
		if !composeServiceName.MatchString(name) || strings.HasPrefix(name, "validation") || strings.HasSuffix(name, "-egress") {
			return nil, composeErr("service name %q is not allowed", name)
		}
		if _, exists := services[name]; exists {
			return nil, composeErr("service %q is defined twice", name)
		}
		service, err := parseComposeService(name, servicesNode.Content[i+1], allowedImages)
		if err != nil {
			return nil, err
		}
		services[name] = service
		order = append(order, name)
	}
	return orderComposeServices(services, order)
}

func rejectAliases(node *yaml.Node) error {
	if node.Kind == yaml.AliasNode || node.Anchor != "" {
		return composeErr("YAML anchors and aliases are not supported")
	}
	for _, child := range node.Content {
		if err := rejectAliases(child); err != nil {
			return err
		}
	}
	return nil
}

func parseComposeService(name string, node *yaml.Node, allowedImages []string) (ComposeService, error) {
	service := ComposeService{Name: name}
	if node.Kind != yaml.MappingNode {
		return service, composeErr("service %q must be a mapping", name)
	}
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i].Value, node.Content[i+1]
		if !slices.Contains(composeServiceKeys, key) {
			return service, composeErr("service %q: %q is not supported", name, key)
		}
		var err error
		switch key {
		case "image":
			service.Image, err = composeImage(value, allowedImages)
		case "environment":
			service.Env, err = composeEnvironment(value)
		case "command":
			service.Command, err = composeCommand(value)
		case "healthcheck":
			service.Health, err = composeHealth(value)
		case "depends_on":
			service.DependsOn, err = composeDependsOn(value)
		case "user":
			service.User = interpolate(value.Value)
			if value.Kind != yaml.ScalarNode || !composeUser.MatchString(service.User) {
				err = composeErr("invalid user")
			}
		case "working_dir":
			service.WorkingDir = interpolate(value.Value)
			if value.Kind != yaml.ScalarNode || !strings.HasPrefix(service.WorkingDir, "/") || strings.ContainsRune(service.WorkingDir, 0) || len(service.WorkingDir) > 255 {
				err = composeErr("working_dir must be an absolute path")
			}
		}
		if err != nil {
			if composeError, ok := err.(*ComposeError); ok {
				return service, composeErr("service %q: %s", name, composeError.Reason)
			}
			return service, err
		}
	}
	if service.Image == "" {
		return service, composeErr("service %q: an image is required (builds are not supported)", name)
	}
	return service, nil
}

// interpolate applies Compose variable syntax with an empty environment: no
// host or manager variable can ever leak into a service definition.
func interpolate(value string) string {
	return composeVariable.ReplaceAllStringFunc(value, func(match string) string {
		if match == "$$" {
			return "$"
		}
		parts := composeVariable.FindStringSubmatch(match)
		if parts[2] != "" {
			return parts[3]
		}
		return ""
	})
}

func composeScalar(node *yaml.Node) (string, error) {
	if node.Kind != yaml.ScalarNode {
		return "", composeErr("expected a single value")
	}
	if strings.Contains(node.Value, "${") && strings.Contains(node.Value, ":?") {
		return "", composeErr("required variables are not supported")
	}
	value := interpolate(node.Value)
	if strings.ContainsRune(value, 0) || len(value) > 8192 {
		return "", composeErr("value is too long or invalid")
	}
	return value, nil
}

func composeImage(node *yaml.Node, allowed []string) (string, error) {
	value, err := composeScalar(node)
	if err != nil {
		return "", err
	}
	named, err := reference.ParseNormalizedNamed(value)
	if err != nil || len(value) > 255 {
		return "", composeErr("invalid image reference")
	}
	repository := named.Name()
	for _, prefix := range allowed {
		prefix = strings.TrimSuffix(strings.TrimSpace(prefix), "/")
		if prefix != "" && (repository == prefix || strings.HasPrefix(repository, prefix+"/")) {
			return reference.TagNameOnly(named).String(), nil
		}
	}
	return "", composeErr("image %q is not in the allowed image list", reference.FamiliarName(named))
}

func composeEnvironment(node *yaml.Node) ([]string, error) {
	env := []string{}
	add := func(name, value string) error {
		if !composeEnvName.MatchString(name) {
			return composeErr("invalid environment variable %q", name)
		}
		env = append(env, name+"="+value)
		return nil
	}
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			value := ""
			if node.Content[i+1].Tag != "!!null" {
				var err error
				if value, err = composeScalar(node.Content[i+1]); err != nil {
					return nil, err
				}
			}
			if err := add(node.Content[i].Value, value); err != nil {
				return nil, err
			}
		}
	case yaml.SequenceNode:
		for _, item := range node.Content {
			entry, err := composeScalar(item)
			if err != nil {
				return nil, err
			}
			name, value, _ := strings.Cut(entry, "=")
			if err := add(name, value); err != nil {
				return nil, err
			}
		}
	default:
		return nil, composeErr("environment must be a mapping or a list")
	}
	if len(env) > 64 {
		return nil, composeErr("too many environment variables")
	}
	return env, nil
}

func composeStrings(node *yaml.Node) ([]string, error) {
	if node.Kind == yaml.ScalarNode {
		value, err := composeScalar(node)
		if err != nil {
			return nil, err
		}
		if strings.ContainsAny(value, `"'\`) {
			return nil, composeErr("quoted string commands are not supported; use a list")
		}
		return strings.Fields(value), nil
	}
	if node.Kind != yaml.SequenceNode {
		return nil, composeErr("expected a string or a list")
	}
	values := make([]string, 0, len(node.Content))
	for _, item := range node.Content {
		value, err := composeScalar(item)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func composeCommand(node *yaml.Node) ([]string, error) {
	command, err := composeStrings(node)
	if err == nil && len(command) > 64 {
		err = composeErr("command has too many arguments")
	}
	return command, err
}

func composeHealth(node *yaml.Node) (*ComposeHealth, error) {
	if node.Kind != yaml.MappingNode {
		return nil, composeErr("healthcheck must be a mapping")
	}
	health := &ComposeHealth{}
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i].Value, node.Content[i+1]
		switch key {
		case "test":
			if value.Kind == yaml.ScalarNode {
				test, err := composeScalar(value)
				if err != nil {
					return nil, err
				}
				health.Test = []string{"CMD-SHELL", test}
				continue
			}
			test, err := composeStrings(value)
			if err != nil {
				return nil, err
			}
			if len(test) == 0 || !slices.Contains([]string{"CMD", "CMD-SHELL", "NONE"}, test[0]) {
				return nil, composeErr("healthcheck test must start with CMD, CMD-SHELL or NONE")
			}
			health.Test = test
		case "interval", "timeout", "start_period":
			duration, err := time.ParseDuration(value.Value)
			if err != nil || duration < 0 || duration > 10*time.Minute {
				return nil, composeErr("invalid healthcheck %s", key)
			}
			switch key {
			case "interval":
				health.Interval = duration
			case "timeout":
				health.Timeout = duration
			default:
				health.StartPeriod = duration
			}
		case "retries":
			var retries int
			if value.Decode(&retries) != nil || retries < 0 || retries > 100 {
				return nil, composeErr("invalid healthcheck retries")
			}
			health.Retries = retries
		case "disable":
			var disabled bool
			if value.Decode(&disabled) != nil {
				return nil, composeErr("invalid healthcheck disable")
			}
			if disabled {
				health.Test = []string{"NONE"}
			}
		default:
			return nil, composeErr("healthcheck %q is not supported", key)
		}
	}
	if len(health.Test) == 0 || health.Test[0] == "NONE" {
		return nil, nil
	}
	return health, nil
}

func composeDependsOn(node *yaml.Node) ([]string, error) {
	switch node.Kind {
	case yaml.SequenceNode:
		return composeStrings(node)
	case yaml.MappingNode:
		names := []string{}
		for i := 0; i < len(node.Content); i += 2 {
			condition := node.Content[i+1]
			if condition.Kind != yaml.MappingNode {
				return nil, composeErr("invalid depends_on entry")
			}
			for j := 0; j < len(condition.Content); j += 2 {
				key, value := condition.Content[j].Value, condition.Content[j+1].Value
				// Every health-checked service must be healthy before the check
				// starts, so both conditions are satisfied by the manager.
				if key == "condition" && value != "service_started" && value != "service_healthy" {
					return nil, composeErr("depends_on condition %q is not supported", value)
				}
				if key != "condition" && key != "required" && key != "restart" {
					return nil, composeErr("depends_on %q is not supported", key)
				}
			}
			names = append(names, node.Content[i].Value)
		}
		return names, nil
	}
	return nil, composeErr("depends_on must be a list or a mapping")
}

func orderComposeServices(services map[string]ComposeService, declared []string) ([]ComposeService, error) {
	ordered := make([]ComposeService, 0, len(services))
	state := map[string]int{}
	var visit func(string) error
	visit = func(name string) error {
		switch state[name] {
		case 1:
			return composeErr("depends_on has a cycle at %q", name)
		case 2:
			return nil
		}
		service, ok := services[name]
		if !ok {
			return composeErr("depends_on references unknown service %q", name)
		}
		state[name] = 1
		for _, dependency := range service.DependsOn {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		state[name] = 2
		ordered = append(ordered, service)
		return nil
	}
	for _, name := range declared {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}
