import { agentModelCatalog, modelServers } from '$lib/api/dev-machines';
import type { CatalogModel, ModelServer } from '$lib/types/dev-machine';
import { catalogStatus, modelServerName, type CatalogStatus } from './model-catalog';

export function hasModelSuggestions(harness?: string): boolean {
	return harness === 'opencode' || harness === 'codex' || harness === 'claude-code';
}

function serverSuggestions(server: ModelServer): CatalogModel[] {
	const ids = server.models.length ? server.models : [server.default_model];
	return ids.map((id) => ({ id, name: modelServerName(server.model_names, id) }));
}

export class ModelSuggestions {
	models = $state<CatalogModel[]>([]);
	status = $state<CatalogStatus>('empty');
	private request = 0;

	open(slug: string, harness: string, server?: ModelServer) {
		const models = server ? serverSuggestions(server) : [];
		this.request++;
		this.models = models;
		this.status = models.length ? 'ready' : 'empty';
		if (harness && !server) void this.load(slug, harness, false);
	}

	async load(slug: string, harness: string, refresh: boolean, server?: ModelServer) {
		const request = ++this.request;
		this.status = 'loading';
		try {
			if (server) {
				const saved = (await modelServers.list(slug)).find(
					(item) => item.id === server.id && item.harnesses.includes(harness)
				);
				if (!saved) throw new Error('Model server is unavailable');
				if (request !== this.request) return;
				this.models = serverSuggestions(saved);
				this.status = this.models.length ? 'ready' : 'empty';
			} else {
				const catalog = await agentModelCatalog(slug, harness, refresh);
				if (request !== this.request) return;
				if (!catalog.unavailable) this.models = catalog.models;
				this.status = catalog.unavailable ? this.failureStatus() : catalogStatus(catalog, false, false);
			}
		} catch {
			if (request === this.request) this.status = this.failureStatus();
		}
	}

	private failureStatus(): CatalogStatus {
		return this.models.length ? 'stale' : 'unavailable';
	}
}
