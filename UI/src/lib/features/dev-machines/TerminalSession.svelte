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
	import { useTerminalDock } from './terminal-dock-context.svelte';
	type SocketConnection = { socket: WebSocket; timer?: ReturnType<typeof setTimeout>; expectedClose: boolean };

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
	let socketConnection: SocketConnection | undefined;
	let gatewayOrigin = $state('');
	let resizeObserver: ResizeObserver | undefined;
	let session = $state<DevMachineTerminalSession | null>(null);
	let status = $state<'idle' | 'creating' | 'resuming' | 'pending' | 'connecting' | 'connected' | 'closed' | 'error'>('idle');
	let statusMessage = $state('');
	let retrying = $state(false);
	let runId = 0;
	const disposables: IDisposable[] = [];
	let priorVisibility = $state(true);
	const dock = useTerminalDock();

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
		status = 'creating';
		statusMessage = id > 1 ? 'Reconnecting terminal...' : 'Creating a terminal session...';
		dock.setRuntimeTitle(tab.id, '');
		await cleanup(true);
		if (id !== runId) return;
		status = 'creating';
		statusMessage = 'Creating a terminal session...';
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
			if (id === runId) retrying = false;
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
		try {
			const recoveryUrl = new URL(webSocketUrl);
			if (recoveryUrl.protocol === 'wss:') recoveryUrl.protocol = 'https:';
			else if (recoveryUrl.protocol === 'ws:') recoveryUrl.protocol = 'http:';
			gatewayOrigin = ['http:', 'https:'].includes(recoveryUrl.protocol) ? recoveryUrl.origin : '';
		} catch {
			gatewayOrigin = '';
		}
		disposeSocketConnection(socketConnection, 'reconnect');
		socketConnection = undefined;
		let activeSocket: WebSocket;
		try {
			activeSocket = new WebSocket(webSocketUrl, ['tty']);
		} catch {
			closeCurrentSession();
			status = 'error';
			statusMessage = 'Unable to connect to the terminal gateway';
			return;
		}
		const connection: SocketConnection = { socket: activeSocket, expectedClose: false };
		socketConnection = connection;
		activeSocket.binaryType = 'arraybuffer';
		connection.timer = setTimeout(() => {
			if (!isCurrentConnection(connection, id) || activeSocket.readyState !== WebSocket.CONNECTING) return;
			failSocketConnection(connection, id, 'Terminal gateway connection timed out. Check the machine TLS certificate and retry.');
		}, 10_000);
		activeSocket.addEventListener('open', () => {
			if (!terminal || !isCurrentConnection(connection, id)) return;
			clearSocketTimer(connection);
			activeSocket.send(encodeInitialTerminalMessage({ columns: terminal.cols, rows: terminal.rows }));
			fitTerminal();
			terminal.clear();
			status = 'connected';
			statusMessage = 'Terminal connected';
			if (visible) terminal.focus();
		});
		activeSocket.addEventListener('message', (event) => {
			if (!terminal || !isCurrentConnection(connection, id)) return;
			const frame = decodeServerFrame(event.data as ArrayBuffer | Uint8Array | string);
			if (frame.command === 'output') {
				terminal.write(frame.data);
			} else if (frame.command === 'title') {
				dock.setRuntimeTitle(tab.id, normalizedWindowTitle(frame.title));
			} else if (frame.command === 'preferences') {
				applyPreferences(frame.preferences);
			}
		});
		activeSocket.addEventListener('close', () => {
			clearSocketTimer(connection);
			if (!isCurrentConnection(connection, id) || connection.expectedClose) return;
			socketConnection = undefined;
			closeCurrentSession();
			status = 'closed';
			statusMessage = 'Terminal connection closed. Reconnect to start a new shell.';
		});
		activeSocket.addEventListener('error', () => {
			if (!isCurrentConnection(connection, id) || connection.expectedClose) return;
			failSocketConnection(connection, id, 'Unable to connect to the terminal gateway. Check the machine TLS certificate and retry.');
		});
	}

	function isCurrentConnection(connection: SocketConnection, id: number) {
		return socketConnection === connection && id === runId;
	}

	function clearSocketTimer(connection: SocketConnection) {
		if (connection.timer) clearTimeout(connection.timer);
		connection.timer = undefined;
	}

	function disposeSocketConnection(connection: SocketConnection | undefined, reason: string) {
		if (!connection) return;
		connection.expectedClose = true;
		clearSocketTimer(connection);
		try {
			connection.socket.close(1000, reason);
		} catch {
			// ignore
		}
	}

	function closeCurrentSession() {
		const disconnectedSession = session;
		session = null;
		dock.setRuntimeTitle(tab.id, '');
		if (disconnectedSession?.id) closeTerminalSession(tab.slug, tab.machineId, disconnectedSession.id).catch(() => {});
	}

	function normalizedWindowTitle(value: string) {
		return value.replace(/[\u0000-\u001f\u007f]/g, ' ').trim().slice(0, 128);
	}

	function failSocketConnection(connection: SocketConnection, id: number, message: string) {
		if (!isCurrentConnection(connection, id)) return;
		socketConnection = undefined;
		disposeSocketConnection(connection, 'connection failed');
		closeCurrentSession();
		status = 'error';
		statusMessage = message;
	}

	function sendFrame(frame: Uint8Array) {
		const activeSocket = socketConnection?.socket;
		if (activeSocket?.readyState === WebSocket.OPEN) activeSocket.send(frame);
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
		const connection = socketConnection;
		socketConnection = undefined;
		disposeSocketConnection(connection, 'ui closed');
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
