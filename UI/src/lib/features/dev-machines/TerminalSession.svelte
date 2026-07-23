<script lang="ts">
	import { browser } from '$app/environment';
	import { onDestroy, tick } from 'svelte';
	import { LoaderCircle, RefreshCw } from 'lucide-svelte';
	import { Alert, AlertDescription } from '$lib/components/ui/alert';
	import { Button } from '$lib/components/ui/button';
	import {
		closeTerminalSession,
		createTerminalSessionWithResume
	} from '$lib/api/dev-machines';
	import type { DevMachineTerminalSession } from '$lib/types/dev-machine';
	import {
		TTYD_PROTOCOL,
		decodeServerFrame,
		encodeInitialTerminalMessage,
		encodeInputFrame,
		encodeResizeFrame
	} from './ttyd';
	import '@xterm/xterm/css/xterm.css';
	import type { Terminal as XTerminal, IDisposable } from '@xterm/xterm';
	import type { FitAddon as XFitAddon } from '@xterm/addon-fit';

	let {
		tab,
		visible = true,
	}: {
		tab: import('./terminal-dock-context.svelte').TerminalTab;
		visible?: boolean;
	} = $props();

	let terminalElement: HTMLDivElement | undefined;
	let terminal: XTerminal | undefined;
	let fitAddon: XFitAddon | undefined;
	let socket: WebSocket | undefined;
	let socketConnectTimer: ReturnType<typeof setTimeout> | undefined;
	let gatewayOrigin = $state('');
	let resizeObserver: ResizeObserver | undefined;
	let session = $state<DevMachineTerminalSession | null>(null);
	let status = $state<'idle' | 'creating' | 'resuming' | 'pending' | 'connecting' | 'connected' | 'closed' | 'error'>('idle');
	let statusMessage = $state('');
	let title = $state('');
	let retrying = $state(false);
	let runId = 0;
	let expectedClose = false;
	const disposables: IDisposable[] = [];
	let priorVisibility = $state(true);

	const canRetry = $derived(status === 'error' || status === 'closed');

	$effect(() => {
		if (!visible) {
			priorVisibility = false;
			return;
		}
		const wasHidden = !priorVisibility;
		priorVisibility = true;

		if (wasHidden && terminal && fitAddon) {
			tick().then(() => {
				if (!terminal || !terminalElement) return;
				fitTerminal();
				terminal.focus();
			});
			return;
		}

		if (runId > 0) return;

		const id = ++runId;
		void start(id);
	});

	onDestroy(() => {
		runId += 1;
		void cleanup(true);
	});

	async function start(id = ++runId) {
		if (!browser || !visible) return;
		retrying = true;
		await cleanup(true);
		if (id !== runId) return;
		status = 'creating';
		statusMessage = 'Creating a terminal session...';
		title = '';
		try {
			await prepareTerminal();
			if (id !== runId) return;
			const launch = await createTerminalSessionWithResume(
				tab.slug,
				tab.machineId,
				{ name: tab.sessionName ?? (tab.checkoutLabel ? `Terminal - ${tab.checkoutLabel}` : 'Terminal'), checkout_id: tab.checkoutId },
				{
					onStatus: (next) => {
						status = next;
						statusMessage = next === 'resuming'
							? 'Resuming the paused Dev Machine...'
							: 'Waiting for the Dev Machine runtime to finish starting...';
					}
				}
			);
			if (id !== runId) {
				if (launch.session?.id) {
					closeTerminalSession(tab.slug, tab.machineId, launch.session.id).catch(() => {});
				}
				return;
			}
			if (launch.protocol !== TTYD_PROTOCOL || !launch.web_socket_url || !launch.session?.id) {
				throw new Error('The terminal gateway did not return a ttyd.v1 WebSocket session');
			}
			session = launch.session;
			status = 'connecting';
			statusMessage = 'Connecting terminal...';
			connectSocket(launch.web_socket_url, id);
		} catch (error) {
			if (id !== runId) return;
			status = 'error';
			statusMessage = error instanceof Error ? error.message : 'Unable to open the terminal';
		} finally {
			retrying = false;
		}
	}

	async function prepareTerminal() {
		const [{ Terminal }, { FitAddon }] = await Promise.all([
			import('@xterm/xterm'),
			import('@xterm/addon-fit')
		]);
		await tick();
		if (!terminalElement) throw new Error('Terminal container is not ready');
		terminal = new Terminal({
			cursorBlink: true,
			convertEol: true,
			fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace',
			fontSize: 13,
			letterSpacing: 0,
			lineHeight: 1.1,
			scrollback: 5000,
			theme: {
				background: '#09090b',
				foreground: '#e4e4e7',
				cursor: '#fafafa',
				selectionBackground: '#3f3f46'
			}
		});
		fitAddon = new FitAddon();
		terminal.loadAddon(fitAddon);
		terminal.open(terminalElement);
		fitTerminal();
		terminal.writeln('\x1b[90mPreparing terminal session...\x1b[0m');
		disposables.push(
			terminal.onData((data) => sendFrame(encodeInputFrame(data))),
			terminal.onBinary((data) => sendFrame(encodeInputFrame(Uint8Array.from(data, (char) => char.charCodeAt(0))))),
			terminal.onResize(({ cols, rows }) => sendFrame(encodeResizeFrame({ columns: cols, rows })))
		);
		resizeObserver = new ResizeObserver(() => fitTerminal());
		resizeObserver.observe(terminalElement);
		window.addEventListener('resize', fitTerminal);
	}

	function connectSocket(webSocketUrl: string, id: number) {
		expectedClose = false;
		try {
			gatewayOrigin = new URL(webSocketUrl).origin;
		} catch {
			gatewayOrigin = '';
		}
		const prevSocket = socket;
		if (prevSocket) {
			try { prevSocket.close(1000, 'reconnect'); } catch { /* ignore */ }
		}
		try {
			socket = new WebSocket(webSocketUrl, ['tty']);
		} catch {
			status = 'error';
			statusMessage = 'Unable to connect to the terminal gateway';
			return;
		}
		const activeSocket = socket;
		activeSocket.binaryType = 'arraybuffer';
		socketConnectTimer = setTimeout(() => {
			if (activeSocket !== socket || activeSocket.readyState !== WebSocket.CONNECTING || id !== runId) return;
			expectedClose = true;
			activeSocket.close();
			status = 'error';
			statusMessage = 'Terminal gateway connection timed out. Check the machine TLS certificate and retry.';
		}, 10_000);
		activeSocket.addEventListener('open', () => {
			if (!terminal || activeSocket !== socket || id !== runId) return;
			if (socketConnectTimer) clearTimeout(socketConnectTimer);
			socketConnectTimer = undefined;
			activeSocket.send(encodeInitialTerminalMessage({ columns: terminal.cols, rows: terminal.rows }));
			fitTerminal();
			terminal.clear();
			status = 'connected';
			statusMessage = 'Terminal connected';
			if (visible) terminal.focus();
		});
		activeSocket.addEventListener('message', (event) => {
			if (!terminal || activeSocket !== socket || id !== runId) return;
			const frame = decodeServerFrame(event.data as ArrayBuffer | Uint8Array | string);
			if (frame.command === 'output') {
				terminal.write(frame.data);
			} else if (frame.command === 'title') {
				title = frame.title;
			} else if (frame.command === 'preferences') {
				applyPreferences(frame.preferences);
			}
		});
		activeSocket.addEventListener('close', () => {
			if (socketConnectTimer) clearTimeout(socketConnectTimer);
			socketConnectTimer = undefined;
			if (activeSocket !== socket || id !== runId || expectedClose) return;
			const disconnectedSession = session;
			session = null;
			if (disconnectedSession?.id) {
				closeTerminalSession(tab.slug, tab.machineId, disconnectedSession.id).catch(() => {});
			}
			status = 'closed';
			statusMessage = 'Terminal connection closed. Reconnect to start a new shell.';
		});
		activeSocket.addEventListener('error', () => {
			if (socketConnectTimer) clearTimeout(socketConnectTimer);
			socketConnectTimer = undefined;
			if (activeSocket !== socket || id !== runId || expectedClose) return;
			status = 'error';
			statusMessage = 'Unable to connect to the terminal gateway. Check the machine TLS certificate and retry.';
		});
	}

	function sendFrame(frame: Uint8Array) {
		if (socket?.readyState === WebSocket.OPEN) socket.send(frame);
	}

	function fitTerminal() {
		try {
			fitAddon?.fit();
		} catch {
			// The terminal can be hidden during transitions.
		}
	}

	function applyPreferences(preferences: Record<string, unknown>) {
		if (!terminal) return;
		const terminalOptions = terminal.options as Record<string, unknown>;
		for (const [key, value] of Object.entries(preferences)) {
			if (key === 'rendererType' || key === 'disableReconnect' || key === 'closeOnDisconnect') continue;
			terminalOptions[key] = value;
		}
		fitTerminal();
	}

	async function cleanup(closeBackendSession: boolean) {
		expectedClose = true;
		if (socketConnectTimer) clearTimeout(socketConnectTimer);
		socketConnectTimer = undefined;
		try {
			socket?.close(1000, 'ui closed');
		} catch {
			// ignore
		}
		socket = undefined;
		resizeObserver?.disconnect();
		resizeObserver = undefined;
		window.removeEventListener('resize', fitTerminal);
		while (disposables.length > 0) {
			try {
				disposables.pop()?.dispose();
			} catch {
				// ignore
			}
		}
		try {
			terminal?.dispose();
		} catch {
			// ignore
		}
		terminal = undefined;
		fitAddon = undefined;
		const sessionToClose = session;
		session = null;
		if (closeBackendSession && sessionToClose?.id) {
			await closeTerminalSession(tab.slug, tab.machineId, sessionToClose.id).catch(() => {});
		}
		if (!visible) {
			status = 'idle';
			statusMessage = '';
			title = '';
		}
	}
</script>

<div class="flex h-full min-h-0 flex-1 flex-col bg-zinc-950" class:hidden={!visible}>
	<div class="flex items-center justify-between gap-3 border-b border-zinc-800 px-3 py-2 text-xs text-zinc-300" aria-live="polite" role="status">
		<span class="flex min-w-0 items-center gap-2">
			{#if status === 'creating' || status === 'resuming' || status === 'pending' || status === 'connecting'}
				<LoaderCircle class="size-3.5 animate-spin" />
			{/if}
			<span class="truncate">{statusMessage || 'Terminal idle'}</span>
		</span>
		{#if canRetry}
			<Button size="xs" variant="outline" disabled={retrying} onclick={() => start()}><RefreshCw class="size-3" />Reconnect</Button>
		{/if}
	</div>
	{#if status === 'error'}
		<div class="p-3">
			<Alert variant="destructive"><AlertDescription>{statusMessage}{#if gatewayOrigin} <a href={gatewayOrigin} target="_blank" rel="noreferrer" class="underline">Open the terminal gateway</a>, accept the local certificate if prompted, then reconnect.{/if}</AlertDescription></Alert>
		</div>
	{/if}
	<div bind:this={terminalElement} class="min-h-0 flex-1 overflow-hidden p-2" data-testid="native-terminal"></div>
</div>
