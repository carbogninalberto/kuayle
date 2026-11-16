import { defineParams } from '@sveltejs/kit/params';

export const params = defineParams({
	/** Matches root-level alternative pages such as `linear-alternative`. */
	alternative: (param) => (/^[a-z0-9]+(?:-[a-z0-9]+)*-alternative$/.test(param) ? param : undefined)
});
