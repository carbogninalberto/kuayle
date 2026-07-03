import { env } from '$env/dynamic/public';

export type DemoUser = {
	label: string;
	email: string;
	password: string;
};

const enabledValues = new Set(['1', 'true', 'yes', 'on']);
const defaultDemoUsers: DemoUser[] = [
	{ label: 'Owner', email: 'alice@kuayle.dev', password: 'Password123!' },
	{ label: 'Admin', email: 'bob@kuayle.dev', password: 'Password123!' },
	{ label: 'Member', email: 'carol@kuayle.dev', password: 'Password123!' },
	{ label: 'Member', email: 'dave@kuayle.dev', password: 'Password123!' },
	{ label: 'Guest', email: 'eve@kuayle.dev', password: 'Password123!' }
];

function parseDemoUsers(value: string | undefined): DemoUser[] {
	if (!value) return defaultDemoUsers;

	try {
		const users = JSON.parse(value) as DemoUser[];
		if (!Array.isArray(users)) return defaultDemoUsers;

		const validUsers = users.filter((user) => user.label && user.email && user.password);
		return validUsers.length > 0 ? validUsers : defaultDemoUsers;
	} catch {
		return defaultDemoUsers;
	}
}

export const demoMode = enabledValues.has((env.PUBLIC_DEMO_MODE ?? '').toLowerCase());
export const demoUsers = parseDemoUsers(env.PUBLIC_DEMO_USERS);
