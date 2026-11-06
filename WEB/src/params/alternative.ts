import type { ParamMatcher } from '@sveltejs/kit';

/** Matches root-level alternative pages such as `linear-alternative`. */
export const match: ParamMatcher = (param) => /^[a-z0-9]+(?:-[a-z0-9]+)*-alternative$/.test(param);
