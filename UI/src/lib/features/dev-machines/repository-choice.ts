import type { CreateDevMachineInput, DevMachineRepositoryDefaults } from '$lib/types/dev-machine';

/** Keeps the repository the development settings resolve on the server. */
export const DEFAULT_REPOSITORY_CHOICE = 'default';

/** The create request's `repo` for a choice: none for the default, so the
 * server applies the issue → project → team → workspace repository itself. */
export function repositoryCreateInput(
	choice: string,
	defaults: DevMachineRepositoryDefaults | null
): CreateDevMachineInput['repo'] {
	if (choice === DEFAULT_REPOSITORY_CHOICE) return undefined;
	const repository = defaults?.repositories.find((item) => item.id === choice);
	if (!repository) return undefined;
	const [owner, name] = repository.full_name.split('/');
	return { provider: 'github', owner, name, url: `https://github.com/${repository.full_name}` };
}

/** The server refuses a default the workspace policy no longer allows. */
export function repositoryChoiceBlocked(choice: string, defaults: DevMachineRepositoryDefaults | null): boolean {
	return choice === DEFAULT_REPOSITORY_CHOICE && defaults?.repository?.allowed === false;
}
