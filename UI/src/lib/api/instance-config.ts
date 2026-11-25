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
}

export interface InstanceConfig {
	groups: string[];
	variables: InstanceConfigVariable[];
}

const path = (name: string) => `/api/system/config/${encodeURIComponent(name)}`;

export const getInstanceConfig = () => api.get<InstanceConfig>('/api/system/config');
export const setInstanceConfigValue = (name: string, value: string) =>
	api.put<InstanceConfigVariable>(path(name), { value });
export const resetInstanceConfigValue = (name: string) => api.delete<InstanceConfigVariable>(path(name));
