import { defineEnvVars } from '@sveltejs/kit/env';

// Public build-time settings (see UI/Dockerfile), read through `$app/env/public`.
// Both are optional: the validator keeps an unset variable `undefined` instead
// of failing the build.
const optional = (value: string | undefined) => value;

export const variables = defineEnvVars({
	PUBLIC_DEMO_MODE: {
		public: true,
		description: 'Enables demo mode (`1`, `true`, `yes` or `on`).',
		schema: optional
	},
	PUBLIC_DEMO_USERS: {
		public: true,
		description: 'JSON array of `{ label, email, password }` sign-in shortcuts shown in demo mode.',
		schema: optional
	}
});
