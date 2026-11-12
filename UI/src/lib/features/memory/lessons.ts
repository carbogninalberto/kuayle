import type { Lesson } from '$lib/api/memory';

// Workspace memory as Settings shows it: proposals wait in one place, and
// only approved lessons are listed under their repository, in the order
// agents read them. Retired and dropped lessons are final and not shown.

export interface LessonBoard {
	proposals: Lesson[];
	repositories: { repository: string; lessons: Lesson[] }[];
	/** The repositories a lesson can be written for. */
	choices: string[];
}

const byName = (a: string, b: string) => a.localeCompare(b, undefined, { sensitivity: 'base' });

export function lessonBoard(lessons: Lesson[], connected: string[] = []): LessonBoard {
	const proposals = lessons.filter((lesson) => lesson.state === 'proposed');
	const groups = new Map<string, { repository: string; lessons: Lesson[] }>();
	const group = (repository: string) => {
		const key = repository.toLowerCase();
		if (!groups.has(key)) groups.set(key, { repository, lessons: [] });
		return groups.get(key)!;
	};
	for (const lesson of lessons) if (lesson.state === 'approved') group(lesson.repository).lessons.push(lesson);
	const repositories = [...groups.values()].sort((a, b) => byName(a.repository, b.repository));
	for (const entry of repositories) entry.lessons.sort((a, b) => a.position - b.position);
	const choices = new Map<string, string>();
	for (const repository of [...repositories.map((entry) => entry.repository), ...proposals.map((l) => l.repository), ...connected])
		if (!choices.has(repository.toLowerCase())) choices.set(repository.toLowerCase(), repository);
	return { proposals, repositories, choices: [...choices.values()].sort(byName) };
}

/** The order with one lesson moved a place up (-1) or down (1); null when it cannot move. */
export function movedLesson(ids: string[], id: string, delta: -1 | 1): string[] | null {
	const from = ids.indexOf(id),
		to = from + delta;
	if (from < 0 || to < 0 || to >= ids.length) return null;
	const next = [...ids];
	[next[from], next[to]] = [next[to], next[from]];
	return next;
}

/** Who wrote the words: an agent, a reviewer on GitHub, or a workspace member. */
export function lessonOrigin(lesson: Pick<Lesson, 'proposer'>): 'agent' | 'github' | 'person' {
	return lesson.proposer === 'external' ? 'github' : lesson.proposer === 'person' ? 'person' : 'agent';
}

/** A person changed the proposed text (the original is only sent to those who decide). */
export function wasEdited(lesson: Pick<Lesson, 'text' | 'proposed_text'>): boolean {
	return !!lesson.proposed_text && lesson.proposed_text !== lesson.text;
}
