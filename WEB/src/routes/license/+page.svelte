<script lang="ts">
	import StandalonePage from '$lib/components/StandalonePage.svelte';
	import { contentModifiedAt, url } from '$lib/config/site';
	import { breadcrumbsFrom } from '$lib/data/routes';

	const meta = {
		title: 'Kuayle License — Apache 2.0',
		description: 'A plain-language summary of Kuayle’s Apache 2.0 license, including permissions, redistribution conditions, patent terms, and warranty limits.',
		canonical: url('/license'),
		modifiedAt: contentModifiedAt('/license')
	};

	const crumbs = breadcrumbsFrom('license', 'License');

	const sections = [
		{
			heading: 'Apache License 2.0',
			body: 'Kuayle is distributed under the Apache License, Version 2.0. The full license text can be found in the LICENSE file at the root of the repository.',
			list: [
				'Copyright (c) 2025-present Bakney srl',
				'Licensed under the Apache License, Version 2.0',
				'Full text: https://www.apache.org/licenses/LICENSE-2.0',
				'Repository: https://github.com/carbogninalberto/kuayle/blob/main/LICENSE'
			]
		},
		{
			heading: 'What you can do',
			body: 'Apache 2.0 is a permissive open-source license. It grants broad rights to use, reproduce, modify and distribute the software, including as part of commercial products, subject to the license conditions.',
			list: [
				'Use Kuayle for any purpose, including commercial use',
				'Modify the source code to fit your needs',
				'Distribute original or modified copies',
				'Include Kuayle code in a larger work',
				'Apply different terms to your modifications where the license permits'
			]
		},
		{
			heading: 'Requirements',
			body: 'Redistribution carries conditions. Read Sections 4 and 6 of the license before shipping Kuayle or a derivative work; the summary below is not legal advice.',
			list: [
				'Include the copyright notice and license in redistributions',
				'State any significant changes made to the original code',
				'Do not use project names or marks to imply endorsement',
				'No warranty — the software is provided "as is"'
			]
		},
		{
			heading: 'Patent grant',
			body: 'Apache 2.0 includes an explicit patent grant from contributors to users. This means anyone who contributes code to Kuayle grants a patent license to anyone who uses that code. This is an important protection that some other permissive licenses (like MIT) do not include.',
			list: [
				'Contributors grant a patent license to users',
				'Protects users against patent claims from contributors',
				'Patent grant terminates if you sue for patent infringement'
			]
		}
	];
</script>

<StandalonePage
	{meta}
	heading="License"
	intro="Kuayle is distributed under the Apache License 2.0. This page is a practical summary only; the repository’s LICENSE file is the authoritative text."
	{sections}
	breadcrumbs={crumbs}
/>
