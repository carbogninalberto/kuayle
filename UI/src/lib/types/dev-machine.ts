export type DevMachineStatus =
	| 'configuring'
	| 'queued'
	| 'spawning'
	| 'running'
	| 'paused'
	| 'stopping'
	| 'stopped'
	| 'tearing_down'
	| 'destroyed'
	| 'failed'
	| 'expired';

type DevMachineLaunchStatus = 'ready' | 'resuming' | 'pending';

type DevMachineEnvironmentStatus = 'pending' | 'building' | 'ready' | 'failed' | 'delete_requested';

interface DevMachineOperation {
	id: string;
	action: string;
	status: 'pending' | 'leased' | 'completed' | 'failed' | 'cancelled' | string;
	generation: number;
	idempotency_key: string;
	attempts: number;
	error_code?: string;
	error_message?: string;
	created_at: string;
	completed_at?: string;
}

type AgentRunStatus =
	| 'queued'
	| 'starting'
	| 'running'
	| 'waiting_input'
	| 'succeeded'
	| 'failed'
	| 'cancelled'
	| 'timeout';

export interface DevMachine {
	id: string;
	workspace_id: string;
	project_id?: string;
	issue_id?: string;
	created_by_user_id?: string;
	routing_key: string;
	name: string;
	status: DevMachineStatus;
	desired_status: DevMachineStatus;
	generation: number;
	repo_url?: string;
	repo_provider?: 'github';
	repo_owner?: string;
	repo_name?: string;
	base_branch?: string;
	working_branch?: string;
	machine_size: 'small' | 'medium' | 'large';
	cpu_millis: number;
	memory_mb: number;
	disk_gb: number;
	pids_limit: number;
	max_runtime_minutes: number;
	environment_id?: string;
	repository_affinity_id?: string;
	keep_running: boolean;
	environment_builder: boolean;
	last_activity_at?: string;
	delete_requested_at?: string;
	docker_network_name?: string;
	workspace_volume_name?: string;
	last_error_code?: string;
	last_error_message?: string;
	created_at: string;
	updated_at: string;
	started_at?: string;
	stopped_at?: string;
	expires_at: string;
	teardown_at?: string;
	destroyed_at?: string;
}

export interface DevMachineService {
	id: string;
	machine_id: string;
	agent_run_id?: string;
	service_type: 'ide' | 'terminal' | 'agent' | 'browser' | 'collector' | 'egress';
	service_key: string;
	container_id?: string;
	container_name: string;
	image_ref: string;
	status: string;
	health_status: string;
	health_message?: string;
	started_at?: string;
}

export interface DevMachineEvent {
	id: number;
	machine_id: string;
	agent_run_id?: string;
	source: string;
	event_type: string;
	payload: Record<string, unknown>;
	occurred_at: string;
}

export interface DevMachineLogChunk {
	id: number;
	agent_run_id?: string;
	service_id?: string;
	stream: 'stdout' | 'stderr' | 'pty' | 'system';
	sequence: number;
	content: string;
	truncated: boolean;
	created_at: string;
}

interface AgentArtifact {
	type: string;
	name: string;
	path: string;
	content_type?: string;
}

interface AgentRunResult {
	status: 'succeeded' | 'failed' | 'cancelled' | 'timeout';
	summary: string;
	changed_files: string[];
	commits: string[];
	branch?: string;
	pull_request_url?: string;
	tests_run: string[];
	test_status: 'passed' | 'failed' | 'not_run';
	risk_notes: string[];
	artifacts: AgentArtifact[];
}

export interface AgentRun {
	id: string;
	machine_id: string;
	issue_id?: string;
	checkout_id?: string;
	provider_id: string;
	mode: 'interactive' | 'autonomous';
	status: AgentRunStatus;
	prompt: string;
	max_runtime_seconds: number;
	push_branch: boolean;
	open_pull_request: boolean;
	result?: AgentRunResult;
	summary?: string;
	changed_files: string[];
	commits: string[];
	branch?: string;
	pull_request_url?: string;
	tests_run: string[];
	test_status: 'passed' | 'failed' | 'not_run';
	risk_notes: string[];
	exit_code?: number;
	error_message?: string;
	created_at: string;
	started_at?: string;
	completed_at?: string;
}

export interface AgentProvider {
	id: 'claude-code' | 'opencode' | 'codex' | 'custom';
	display_name: string;
	default_image: string;
	required_secrets: string[];
	supported_modes: Array<'interactive' | 'autonomous'>;
	custom: boolean;
}

export interface ResourceSample {
	id: number;
	cpu_percent: number;
	memory_bytes: number;
	disk_bytes: number;
	pids: number;
	network_rx_bytes: number;
	network_tx_bytes: number;
	created_at: string;
}

export interface CreateDevMachineInput {
	name?: string;
	issue_id?: string;
	project_id?: string;
	repo?: { provider: 'github'; owner: string; name: string; url: string };
	base_branch?: string;
	working_branch?: string;
	size: 'small' | 'medium' | 'large';
	services: { ide: boolean; browser: boolean };
	agents: Array<{ provider: AgentProvider['id']; mode: 'interactive' | 'autonomous'; config?: unknown }>;
	env_vars: Array<{
		name: string;
		value: string;
		target_service: 'ide' | 'agent' | 'collector';
		provider?: string;
		secret?: boolean;
	}>;
	environment_id?: string;
	keep_running: boolean;
	environment_builder?: boolean;
}

export interface CreateAgentRunInput {
	checkout_id?: string;
	use_root_workspace: boolean;
	provider: AgentProvider['id'];
	mode: 'interactive' | 'autonomous';
	prompt: string;
	acceptance_criteria: string[];
	allowed_commands: string[];
	forbidden_paths: string[];
	test_command: string[];
	max_runtime_seconds: number;
	allowed_secrets: string[];
	push_branch: boolean;
	open_pull_request: boolean;
}

export interface DevMachinePolicy {
	workspace_id: string;
	enabled: boolean;
	max_concurrent_machines: number;
	max_machines_per_user: number;
	max_daily_agent_runs: number;
	max_runtime_minutes: number;
	max_disk_gb: number;
	allowed_providers: string[];
	allowed_repositories: string[];
	allow_custom_providers: boolean;
	idle_pause_minutes: number;
}

export interface DevMachineCheckout {
	id: string;
	workspace_id: string;
	machine_id: string;
	issue_id: string;
	github_repo_id: string;
	repository_full_name: string;
	base_branch: string;
	working_branch: string;
	workspace_path: string;
	status: 'queued' | 'preparing' | 'ready' | 'failed';
	last_error?: string;
	created_at: string;
	updated_at: string;
	last_activity_at?: string;
}

export interface DevMachineEnvironment {
	id: string;
	workspace_id: string;
	name: string;
	image_ref: string;
	image_digest?: string;
	status: DevMachineEnvironmentStatus;
	source_machine_id?: string;
	created_by_user_id?: string;
	delete_requested_at?: string;
	created_at: string;
	updated_at: string;
}

export interface DevMachineScopeSetting {
	id?: string;
	workspace_id: string;
	team_id?: string;
	project_id?: string;
	issue_id?: string;
	github_repo_id?: string;
	base_branch?: string;
	environment_id?: string;
	created_at?: string;
	updated_at?: string;
}

export interface DevMachineTerminalSession {
	id: string;
	machine_id: string;
	checkout_id?: string;
	name: string;
	runtime_session_name: string;
	status: 'active' | 'closed' | string;
	created_at: string;
	last_activity_at: string;
	closed_at?: string;
}

export interface LaunchMachineServiceResponse {
	status: DevMachineLaunchStatus;
	launch_url?: string;
	expires_at?: string;
	operation?: DevMachineOperation;
	retry_after_seconds?: number;
}

export interface CreateTerminalSessionInput {
	name?: string;
	checkout_id?: string;
}

export interface TerminalSessionLaunchResponse {
	status: DevMachineLaunchStatus;
	session?: DevMachineTerminalSession;
	launch_url?: string;
	web_socket_url?: string;
	protocol?: 'ttyd.v1' | string;
	expires_at?: string;
	operation?: DevMachineOperation;
	retry_after_seconds?: number;
}

export interface BulkPermanentDeleteDevMachinesInput {
	machine_ids?: string[];
	older_than_days?: number;
	include_failed: boolean;
	include_expired: boolean;
}

export interface AgentRunStep {
	id: string;
	agent_run_id: string;
	sequence: number;
	step_type: string;
	name: string;
	status: 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled' | 'skipped';
	command_argv?: string[];
	summary?: string;
	exit_code?: number;
	started_at?: string;
	completed_at?: string;
	created_at: string;
}

export interface AgentRunTrace {
	run: AgentRun;
	steps: AgentRunStep[];
	events: DevMachineEvent[];
	logs: DevMachineLogChunk[];
	next_event_id: number;
	next_log_id: number;
	has_more_events: boolean;
	has_more_logs: boolean;
}
