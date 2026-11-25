import { Bot, Server } from '@lucide/svelte';
import { m } from '$lib/features/i18n/messages';

// The places the assistant's sidebar links to, also on its rail.
export const assistantPlaces = (slug: string) => [
	{ href: `/${slug}/settings/agents`, label: m['settings.nav.agents'](), icon: Bot },
	{ href: `/${slug}/machines`, label: m['machines.title'](), icon: Server }
];
