// Use the Compose definitions at the checked-out revision, including provider args.
target "runtime-integration" {
  matrix = {
    image = ["headless", "egress"]
  }
  name       = "integration-${image}"
  inherits   = ["dev-machine-${image}"]
  tags       = ["kuayle/dev-machine-${image}:0.1.0"]
  platforms  = ["linux/amd64"]
  cache-from = ["type=gha,scope=dev-machine-${image}-amd64"]
}
