import { api } from './client';

/** Where an effective value comes from: .env, the built-in default, or a sysadmin's change. */
export type InstanceConfigSource = 'environment' | 'default' | 'override';

/** One environment variable the API reads (GET /api/system/config, sysadmins only). */
export interface InstanceConfigVariable {
	name: string;
	group: string;
	description: string;
	kind: 'string' | 'bool' | 'int' | 'float' | 'duration';
	/** A secret never carries its value: only `set`. */
	secret: boolean;
	set: boolean;
	value?: string;
	/** A plain value hidden because it carries credentials (a URL with a user or a query). */
	withheld?: boolean;
	default?: string;
	source: InstanceConfigSource;
	/** Tunable variables change at runtime; a reset returns them to `base_value`. */
	tunable: boolean;
	base_value?: string;
	base_source?: 'environment' | 'default';
	min?: string;
	max?: string;
	updated_at?: string;
	/** Read at startup: editable with an apply-and-restart. `base_value` is what removing the change returns to. */
	restart_editable: boolean;
	rule?: InstanceConfigRule;
}

/** What a restart-editable variable accepts, for its input; the server checks every value again. */
export interface InstanceConfigRule {
	format?: 'text' | 'url' | 'host' | 'email';
	choices?: string[];
	min?: string;
	max?: string;
	allow_empty?: boolean;
}

export type InstanceRestartStatus = 'restarting' | 'applying' | 'confirmed' | 'rolled_back';

export interface InstanceRestartChange {
	name: string;
	from: string;
	to: string;
}

/** One "Apply and restart" of restart-only settings. */
export interface InstanceRestartRun {
	id: string;
	status: InstanceRestartStatus;
	requested_by?: string;
	requested_at: string;
	finished_at?: string;
	failure?: string;
	changes: InstanceRestartChange[];
}

export interface InstanceRestart {
	/** False when nothing would start the API again (RESTART_SUPERVISED is not set). */
	supported: boolean;
	run?: InstanceRestartRun;
	/** Stored settings this process did not use. */
	notice?: string;
}

export interface InstanceConfig {
	groups: string[];
	variables: InstanceConfigVariable[];
	restart: InstanceRestart;
}

/** A string sets a restart-only variable, null returns it to its .env value. */
export type InstanceRestartProposal = Record<string, string | null>;

export interface InstanceRestartIssue {
	/** Empty for a problem of the configuration as a whole. */
	name?: string;
	message: string;
}

/** The dry run: nothing is stored. */
export interface InstanceRestartPlan {
	valid: boolean;
	changes: InstanceRestartChange[];
	issues: InstanceRestartIssue[];
}

const path = (name: string) => `/api/system/config/${encodeURIComponent(name)}`;

export const getInstanceConfig = () => api.get<InstanceConfig>('/api/system/config');
export const setInstanceConfigValue = (name: string, value: string) =>
	api.put<InstanceConfigVariable>(path(name), { value });
export const resetInstanceConfigValue = (name: string) => api.delete<InstanceConfigVariable>(path(name));
export const validateInstanceRestart = (changes: InstanceRestartProposal) =>
	api.post<InstanceRestartPlan>('/api/system/config/restart/validate', { changes });
/** Stores the values and restarts the API; answers 202 with the run, which the page then polls. */
export const applyInstanceRestart = (changes: InstanceRestartProposal) =>
	api.post<InstanceRestartRun>('/api/system/config/restart/apply', { changes });
