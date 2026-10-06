import { chromium } from 'playwright';
import { expect } from '@playwright/test';
import { writeFile, mkdir } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { join } from 'node:path';
const evidenceDir=fileURLToPath(new URL('../../../tmp/reports/',import.meta.url));
await mkdir(evidenceDir,{recursive:true});
const evidence=(name)=>join(evidenceDir,name);
// Opt-in: use an isolated, migrated backend database. No route mocks.
const baseURL=process.env.KUAYLE_LIVE_URL ?? 'http://127.0.0.1:4176';
const browser=await chromium.launch({headless:true});
const owner=await browser.newContext({baseURL,viewport:{width:1440,height:1000}});
const member=await browser.newContext({baseURL,viewport:{width:1280,height:900}});
const suffix=Date.now(); const slug=`live61-${suffix}`; const api=`/api/workspaces/${slug}`;
const errors=[]; const frames=[]; const sockets=[];
const op=await owner.newPage(); const mp=await member.newPage();
for (const [label,page] of [['owner',op],['member',mp]]) {
 page.on('pageerror',e=>errors.push(`${label}: ${e.message}`));
 page.on('websocket',ws=>{sockets.push({label,url:ws.url()});ws.on('framereceived',f=>frames.push({label,payload:String(f.payload)}));ws.on('close',()=>frames.push({label,closed:true}));});
}
async function request(ctx,method,path,data){const r=await ctx.request.fetch(path,{method,data}); if(!r.ok())throw new Error(`${method} ${path}: ${r.status()} ${await r.text()}`);return (await r.text())?r.json():null;}
let workspace;
try {
 const ownerUser=await request(owner,'POST','/api/auth/register',{name:'Live owner',email:`owner-${suffix}@test.invalid`,password:'Local-verification-61!'});
 const memberUser=await request(member,'POST','/api/auth/register',{name:'Live member',email:`member-${suffix}@test.invalid`,password:'Local-verification-61!'});
 workspace=await request(owner,'POST','/api/workspaces',{name:'Live privacy verification',slug});
 await request(owner,'POST',`${api}/invite`,{email:memberUser.email,role:'member'});
 const team=await request(owner,'POST',`${api}/teams`,{name:'Live confidential team',key:'LIV'});
 const issue=await request(owner,'POST',`${api}/issues`,{team_id:team.id,title:'Live confidential title',description:'<p>Live protected description</p>'});
 console.log('Created real API fixture', {workspace:workspace.id,team:team.id,issue:issue.identifier});
 await mp.goto(`/${slug}/issue/${issue.identifier}`);
 await expect(mp.getByText('Live confidential title',{exact:true}).first()).toBeVisible({timeout:30000});
 await op.goto(`/${slug}/settings/teams/${team.id}`);
 await expect(op.getByRole('button',{name:'Make private',exact:true})).toBeVisible({timeout:30000});
 await expect.poll(()=>sockets.filter(s=>s.url.endsWith('/ws')).length).toBeGreaterThanOrEqual(2);
 await op.getByRole('button',{name:'Make private',exact:true}).click();
 await op.getByRole('dialog').getByRole('checkbox').check();
 await op.getByRole('dialog').getByRole('button',{name:'Save',exact:true}).click();
 await expect(op.getByRole('button',{name:'Make public',exact:true})).toBeVisible();
 await expect(mp.getByText('Live confidential title',{exact:true})).toHaveCount(0,{timeout:15000});
 let denied=await member.request.get(`${api}/issues/${issue.identifier}`); expect(denied.status()).toBe(404);
 console.log('PASS public-to-private via UI clears open member issue over real WebSocket; HTTP 404');
 await op.getByRole('combobox',{name:'Choose a workspace member'}).selectOption(memberUser.id);
 await op.getByRole('button',{name:'Add member',exact:true}).click();
 await expect(op.getByRole('button',{name:'Remove Live member from team',exact:true})).toBeVisible();
 await mp.goto(`/${slug}/issue/${issue.identifier}`);
 await expect(mp.getByText('Live confidential title',{exact:true}).first()).toBeVisible({timeout:15000});
 const frameStart=frames.length;
 await op.getByRole('button',{name:'Remove Live member from team',exact:true}).click();
 await expect(op.getByRole('button',{name:'Remove Live member from team',exact:true})).toHaveCount(0);
 await expect(mp.getByText('Live confidential title',{exact:true})).toHaveCount(0,{timeout:15000});
 denied=await member.request.get(`${api}/issues/${issue.identifier}`); expect(denied.status()).toBe(404);
 const removalFrames=frames.slice(frameStart).filter(f=>f.label==='member'&&f.payload);
 expect(removalFrames.some(f=>JSON.parse(f.payload).type==='app.refresh')).toBe(true);
 expect(removalFrames.some(f=>f.payload.includes('Live confidential title')||f.payload.includes('Live protected description'))).toBe(false);
 console.log('PASS member grant/removal via UI, active socket refresh contains no private content, HTTP 404');
 await op.screenshot({path:evidence('issue-61-live-owner.png'),fullPage:true});
 await mp.screenshot({path:evidence('issue-61-live-revoked.png'),fullPage:true});
 await op.getByRole('button',{name:'Make public',exact:true}).click();
 await op.getByRole('dialog').getByRole('checkbox').check();
 await op.getByRole('dialog').getByRole('button',{name:'Save',exact:true}).click();
 await expect(op.getByRole('button',{name:'Make private',exact:true})).toBeVisible();
 await mp.goto(`/${slug}/issue/${issue.identifier}`);
 await expect(mp.getByText('Live confidential title',{exact:true}).first()).toBeVisible();
 expect((await request(owner,'GET',api)).privacy_enabled).toBe(true);
 console.log('PASS explicit declassification restores public access and keeps sticky privacy mode');
 const beforeWorkspaceRemoval=frames.length;
 await request(owner,'DELETE',`${api}/members/${memberUser.id}`);
 await expect.poll(()=>frames.slice(beforeWorkspaceRemoval).some(f=>f.label==='member'&&f.closed),{timeout:15000}).toBe(true);
 await expect(mp.getByText('Live confidential title',{exact:true})).toHaveCount(0,{timeout:15000});
 denied=await member.request.get(`${api}/issues/${issue.identifier}`); expect([403,404]).toContain(denied.status());
 console.log('PASS workspace removal closes existing WebSocket, clears page, rejects subsequent HTTP');
 expect(errors).toEqual([]);
 await writeFile(evidence('issue-61-live-browser-evidence.json'),JSON.stringify({timestamp:new Date().toISOString(),fixture:{workspace:workspace.id,owner:ownerUser.id,member:memberUser.id},sockets,frames,pageErrors:errors},null,2));
 console.log('PASS real backend + PostgreSQL browser workflow; zero page errors; no HTTP or WebSocket mocks');
} catch(e){ await writeFile(evidence('issue-61-live-browser-failure.json'),JSON.stringify({sockets,frames,pageErrors:errors},null,2)); await op.screenshot({path:evidence('issue-61-live-failure-owner.png'),fullPage:true});await mp.screenshot({path:evidence('issue-61-live-failure-member.png'),fullPage:true});throw e; }
finally{if(workspace)await request(owner,'DELETE',api);await browser.close();}
