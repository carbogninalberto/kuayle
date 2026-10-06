<script lang="ts">
 import { onMount } from 'svelte';
 import type { Team, TeamMember } from '$lib/types/team';
 import type { WorkspaceMember } from '$lib/types/workspace';
 import { getWorkspace } from '$lib/api/workspaces';
 import { listMembers } from '$lib/api/members';
 import { listTeamMembers, addTeamMember, removeTeamMember, setTeamVisibility } from '$lib/api/teams';
 import { Button } from '$lib/components/ui/button';
 import * as Dialog from '$lib/components/ui/dialog';
 import { m } from '$lib/paraglide/messages.js';
 import { appToast } from '$lib/features/toast/toast';
 import PrivacyNotice from './PrivacyNotice.svelte';
 let { slug, team, onupdated }: { slug: string; team: Team; onupdated: (team: Team) => void } = $props();
 let canManage = $state(false);
 let members = $state<TeamMember[]>([]);
 let workspaceMembers = $state<WorkspaceMember[]>([]);
 let loading = $state(true);
 let busy = $state(false);
 let selected = $state('');
 let confirmOpen = $state(false);
 let acknowledged = $state(false);
 let targetPrivate = $state(false);
 let reload = $state(0);
 let generation = 0;
 let disposed = false;
 const candidates = $derived(workspaceMembers.filter(u => !members.some(member => member.user_id === u.user_id)));
 const nameOf = (id: string) => workspaceMembers.find(u => u.user_id === id)?.name ?? id;
 $effect(() => {
  const s = slug, id = team.id;
  reload;
  const version = ++generation;
  let active = true;
  canManage = false; members = []; workspaceMembers = []; selected = ''; loading = true;
  void (async () => {
   try {
    const workspace = await getWorkspace(s);
    if (!active || version !== generation) return;
    canManage = ['owner', 'admin'].includes(workspace.current_user_role);
    if (!canManage) return;
    const [teamMembers, allMembers] = await Promise.all([listTeamMembers(s, id), listMembers(s)]);
    if (!active || version !== generation) return;
    members = teamMembers; workspaceMembers = allMembers;
   } catch (error) {
    if (active && version === generation) appToast.apiError(error, m['privacy.error']());
   } finally { if (active && version === generation) loading = false; }
  })();
  return () => { active = false; };
 });
 onMount(() => {
  const refresh = (event: Event) => {
   const detail = (event as CustomEvent).detail;
   if ((!detail?.slug || detail.slug === slug) && (detail?.resources ?? []).some((r: string) => ['teams', 'members', 'workspace'].includes(r))) reload++;
  };
  window.addEventListener('app:refresh', refresh);
  return () => { disposed = true; window.removeEventListener('app:refresh', refresh); };
 });
 function openConfirmation() { targetPrivate = !team.is_private; acknowledged = false; confirmOpen = true; }
 async function changeVisibility() {
  if (!canManage || !acknowledged || busy) return;
  const s = slug, id = team.id;
  busy = true;
  try {
   const updated = await setTeamVisibility(s, id, targetPrivate);
   if (disposed || slug !== s || team.id !== id) return;
   onupdated(updated); confirmOpen = false;
  } catch (error) { if (!disposed && slug === s && team.id === id) appToast.apiError(error, m['privacy.error']()); }
  finally { if (!disposed && slug === s && team.id === id) busy = false; }
 }
 async function changeMember(userId: string, remove: boolean) {
  if (!canManage || busy || !userId) return;
  const s = slug, id = team.id;
  busy = true;
  try {
   if (remove) await removeTeamMember(s, id, userId); else await addTeamMember(s, id, userId);
   if (!disposed && slug === s && team.id === id) reload++;
  } catch (error) { if (!disposed && slug === s && team.id === id) appToast.apiError(error, m['privacy.error']()); }
  finally { if (!disposed && slug === s && team.id === id) busy = false; }
 }
</script>
<section class="mt-6 space-y-4 rounded-lg border border-[var(--app-border)] bg-[var(--color-bg-secondary)] p-5" aria-label={m['privacy.visibility']()}>
 <div class="flex flex-wrap items-center justify-between gap-3">
  <h2 class="text-sm font-medium">{m['privacy.visibility']()}: {team.is_private ? m['privacy.private']() : m['privacy.public']()}</h2>
  {#if canManage}<Button variant="outline" size="sm" onclick={openConfirmation} disabled={busy || loading}>{team.is_private ? m['privacy.make_public']() : m['privacy.make_private']()}</Button>{/if}
 </div>
 <p class="text-xs text-[var(--color-text-secondary)]">{team.is_private ? m['privacy.policy']() : m['privacy.public_description']()}</p>
 {#if canManage}
  <h3 class="text-sm font-medium">{m['privacy.members']()}</h3>
  {#if loading}<p role="status" class="text-xs">{m['common.loading']()}</p>{:else}
   <ul class="space-y-2">
    {#each members as member (member.user_id)}
     <li class="flex items-center justify-between gap-3 text-sm"><span class="break-words">{nameOf(member.user_id)}</span><Button size="sm" variant="ghost" disabled={busy} aria-label={m['privacy.remove_member']({ name: nameOf(member.user_id) })} onclick={() => changeMember(member.user_id, true)}>{m['common.remove']()}</Button></li>
    {/each}
   </ul>
   <div class="flex flex-wrap gap-2">
    <select bind:value={selected} aria-label={m['privacy.choose_member']()} disabled={busy} class="min-w-0 flex-1 rounded border border-[var(--app-border)] bg-[var(--color-bg)] p-2 text-sm">
     <option value="">{m['privacy.choose_member']()}</option>
     {#each candidates as member (member.user_id)}<option value={member.user_id}>{member.name} ({member.role})</option>{/each}
    </select>
    <Button size="sm" disabled={busy || !selected} onclick={() => changeMember(selected, false)}>{m['privacy.add_member']()}</Button>
   </div>
  {/if}
 {/if}
</section>
<Dialog.Root bind:open={confirmOpen}>
 <Dialog.Content class="max-h-[85dvh] overflow-y-auto sm:max-w-xl">
  <Dialog.Header><Dialog.Title>{targetPrivate ? m['privacy.make_private']() : m['privacy.make_public']()}</Dialog.Title><Dialog.Description>{targetPrivate ? m['privacy.policy']() : m['privacy.public_confirmation']()}</Dialog.Description></Dialog.Header>
  {#if targetPrivate}<PrivacyNotice />{/if}
  <label class="flex items-start gap-2 text-sm"><input type="checkbox" bind:checked={acknowledged} disabled={busy} class="mt-1" /><span>{targetPrivate ? m['privacy.acknowledge']() : m['privacy.confirm_public']()}</span></label>
  <Dialog.Footer><Button variant="outline" disabled={busy} onclick={() => confirmOpen = false}>{m['common.cancel']()}</Button><Button disabled={busy || !acknowledged || !canManage} onclick={changeVisibility}>{busy ? m['team_settings.saving']() : m['common.save']()}</Button></Dialog.Footer>
 </Dialog.Content>
</Dialog.Root>
