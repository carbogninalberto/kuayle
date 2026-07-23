<script lang="ts">
	import Nav from '$lib/components/Nav.svelte';
	import Footer from '$lib/components/Footer.svelte';
	import Seo from '$lib/components/Seo.svelte';
	import Breadcrumbs from '$lib/components/Breadcrumbs.svelte';
	import { consent } from '$lib/cookieConsent.svelte';
	import { Button } from '$lib/components/ui/button';
	import { contentModifiedAt, url } from '$lib/config/site';
	import { breadcrumbsFrom, webPageLd } from '$lib/data/routes';

	let resetOpen = $state(false);

	const title = 'Privacy Policy — Kuayle';
	const description =
		'Kuayle privacy policy: what we collect, the cookies we use, your GDPR rights, and how to contact us.';
	const updated = 'July 11, 2026';
	const canonical = url('/privacy');
	const crumbs = breadcrumbsFrom('privacy', 'Privacy');
	const modifiedAt = contentModifiedAt('/privacy');
	const jsonLd = webPageLd(title, description, canonical, crumbs, modifiedAt);
</script>

<Seo meta={{ title, description, canonical, ogType: 'article', modifiedAt, jsonLd }} />

<Nav />

<main class="mx-auto max-w-3xl px-6 pt-28 pb-20">
	<article>
		<div class="mb-8">
			<Breadcrumbs breadcrumbs={crumbs} />
		</div>
		<p class="text-sm font-semibold tracking-widest text-brand-300 uppercase">Legal</p>
		<h1 class="mt-2 text-4xl font-bold tracking-tight">Privacy Policy</h1>
		<p class="mt-2 text-sm text-muted-foreground">Last updated: {updated}</p>

		<div class="prose-custom mt-10 space-y-10 text-sm leading-relaxed text-muted-foreground">
			<section>
				<h2>1. Who we are</h2>
				<p>
					Kuayle is an open source project maintained by
					<strong class="text-foreground">Bakney srl</strong> (P. IVA 04921790236), with
					registered office at Q.re Aldo Moro 49, 37032 Monteforte d'Alpone (VR), Italy. We are the
					data controller for any personal data processed through this website.
				</p>
				<p>
				For data protection enquiries contact us at
				<a href="mailto:support@bakney.com" class="text-brand-300 hover:underline"
					>support@bakney.com</a
				>.
				</p>
			</section>

			<section>
				<h2>2. What data we collect</h2>
				<ul class="ml-5 list-disc space-y-1">
					<li>
						<strong class="text-foreground">Data you provide:</strong> the contact email address
						you give us when writing to support or requesting a demo.
					</li>
					<li>
						<strong class="text-foreground">Request data:</strong> the server or hosting provider may
						process IP address, request path, user agent, referrer and timestamp in standard access or
						security logs.
					</li>
					<li>
						<strong class="text-foreground">Self-hosted deployments:</strong> when you run Kuayle
						on your own infrastructure, <em>you</em> are the data controller for the data entered
						into your instance. This policy does not cover self-hosted deployments.
					</li>
				</ul>
			</section>

			<section>
				<h2>3. Cookies</h2>
				<p>
					The marketing site currently installs no analytics, advertising or campaign-measurement
					scripts. Your consent selection is stored in browser local storage under
					<code>kuayle_cookie_consent</code>; it is not sent to Kuayle by the site.
				</p>
				<table class="mt-4 w-full border-collapse text-left">
					<thead>
						<tr class="border-b border-white/10 text-foreground">
							<th class="py-2 pr-4 font-medium">Category</th>
							<th class="py-2 pr-4 font-medium">Purpose</th>
							<th class="py-2 font-medium">Current status</th>
						</tr>
					</thead>
					<tbody class="align-top">
						<tr class="border-b border-white/5">
							<td class="py-2 pr-4">Essential</td>
							<td class="py-2 pr-4">Store your consent selection in this browser.</td>
							<td class="py-2">Local storage only</td>
						</tr>
						<tr class="border-b border-white/5">
							<td class="py-2 pr-4">Analytics</td>
							<td class="py-2 pr-4">Reserved for optional usage measurement.</td>
							<td class="py-2">No script installed</td>
						</tr>
						<tr class="border-b border-white/5">
							<td class="py-2 pr-4">Preferences</td>
							<td class="py-2 pr-4">Reserved for non-essential interface settings.</td>
							<td class="py-2">No feature installed</td>
						</tr>
						<tr>
							<td class="py-2 pr-4">Marketing</td>
							<td class="py-2 pr-4">Reserved for optional campaign measurement.</td>
							<td class="py-2">No script installed</td>
						</tr>
					</tbody>
				</table>
				<p class="mt-4">
					If optional measurement tools are introduced, they must remain disabled until the relevant
					consent is given and this policy is updated with the provider, purpose and retention period.
				</p>
			</section>

			<section>
				<h2>4. Legal basis (GDPR Art. 6)</h2>
				<ul class="ml-5 list-disc space-y-1">
					<li>
						<strong class="text-foreground">Consent (Art. 6(1)(a)):</strong> for any optional
						measurement technology introduced in the future. You can withdraw consent at any time.
					</li>
					<li>
						<strong class="text-foreground">Legitimate interest (Art. 6(1)(f)):</strong> for
						security, abuse prevention and ordinary server logging.
					</li>
					<li>
						<strong class="text-foreground">Contract or pre-contract steps (Art. 6(1)(b)):</strong>
						where your request relates to a contract or requested service.
					</li>
				</ul>
			</section>

			<section>
				<h2>5. How long we keep data</h2>
				<p>
					Contact correspondence is kept for as long as needed to handle the request and meet
					applicable legal obligations. Hosting logs are retained according to the hosting and
					security configuration. The consent record remains in your browser until you reset it or
					clear site data.
				</p>
			</section>

			<section>
				<h2>6. Your rights</h2>
				<p>Under the GDPR you have the right to:</p>
				<ul class="ml-5 list-disc space-y-1">
					<li>Access the personal data we hold about you;</li>
					<li>Rectify inaccurate or incomplete data;</li>
					<li>Erase your data (“right to be forgotten”);</li>
					<li>Restrict or object to processing;</li>
					<li>Data portability;</li>
					<li>Withdraw consent at any time without affecting prior processing;</li>
					<li>Lodge a complaint with the competent supervisory authority
						(<a
							href="https://www.garanteprivacy.it"
							target="_blank"
							rel="noopener"
							class="text-brand-300 hover:underline">Garante per la protezione dei dati personali</a
						>).</li>
				</ul>
				<p class="mt-2">
				To exercise any of these rights, email
				<a href="mailto:support@bakney.com" class="text-brand-300 hover:underline"
					>support@bakney.com</a
				>.
				</p>
			</section>

			<section>
				<h2>7. International transfers</h2>
				<p>
					Bakney is based in Italy. If a service provider processes personal data outside the EEA,
					we use an applicable transfer mechanism, such as an adequacy decision or Standard
					Contractual Clauses, where required.
				</p>
			</section>

			<section>
				<h2>8. Children</h2>
				<p>Kuayle is not directed at children under 16, and we do not knowingly collect their data.</p>
			</section>

			<section>
				<h2>9. Changes to this policy</h2>
				<p>
					We may update this policy when the website, providers or legal requirements change. The
					“last updated” date above identifies the current version.
				</p>
			</section>

			<section>
				<h2>10. Manage your preferences</h2>
				<p>You can review or reset your cookie consent at any time.</p>
				<div class="mt-4 flex flex-wrap gap-2">
					{#if consent.decided}
						<span class="inline-flex items-center rounded-md border border-white/10 bg-white/5 px-3 py-1.5 text-xs text-muted-foreground">
							Saved choice: analytics {consent.value.analytics ? 'on' : 'off'}, preferences
							{consent.value.preferences ? 'on' : 'off'}, marketing
							{consent.value.marketing ? 'on' : 'off'}
						</span>
					{/if}
					<Button
						variant="outline"
						size="sm"
						class="border-border"
						onclick={() => (resetOpen = true)}>Reset cookie consent</Button
					>
				</div>
			</section>
		</div>
	</article>
</main>

{#if resetOpen}
	<div
		class="fixed inset-0 z-[60] flex items-center justify-center bg-black/70 p-4 backdrop-blur-sm"
	>
		<button
			type="button"
			class="absolute inset-0 cursor-default"
			aria-label="Close reset confirmation"
			onclick={() => (resetOpen = false)}
		></button>
		<div
			class="relative w-full max-w-sm rounded-2xl border border-white/10 bg-card p-6 shadow-2xl"
			role="dialog"
			aria-modal="true"
			aria-labelledby="reset-title"
			tabindex="-1"
		>
			<h2 id="reset-title" class="text-lg font-semibold">Reset consent?</h2>
			<p class="mt-2 text-sm text-muted-foreground">
				This clears your saved choice. The cookie banner will be shown again on your next visit.
			</p>
			<div class="mt-6 flex items-center justify-end gap-2">
				<Button variant="ghost" size="sm" onclick={() => (resetOpen = false)}>Cancel</Button>
				<Button
					size="sm"
					class="bg-destructive/80 text-white hover:bg-destructive"
					onclick={() => {
						consent.reset();
						resetOpen = false;
					}}>Reset</Button
				>
			</div>
		</div>
	</div>
{/if}

<Footer />
