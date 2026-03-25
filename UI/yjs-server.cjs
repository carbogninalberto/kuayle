// Minimal Y.js WebSocket server for collaborative editing
const http = require('http');
const { WebSocketServer } = require('ws');
const Y = require('yjs');
const syncProtocol = require('y-protocols/sync');
const awarenessProtocol = require('y-protocols/awareness');
const encoding = require('lib0/encoding');
const decoding = require('lib0/decoding');

const PORT = 4444;
const docs = new Map();

const messageSync = 0;
const messageAwareness = 1;

function getDoc(name) {
	if (docs.has(name)) return docs.get(name);
	const doc = new Y.Doc();
	const awareness = new awarenessProtocol.Awareness(doc);
	const entry = { doc, awareness, conns: new Set() };
	docs.set(name, entry);
	return entry;
}

function send(conn, message) {
	if (conn.readyState === conn.OPEN) {
		conn.send(message, (err) => { if (err) console.error(err); });
	}
}

const server = http.createServer((req, res) => {
	res.writeHead(200, { 'Content-Type': 'text/plain' });
	res.end('yjs-server');
});

const wss = new WebSocketServer({ server });

wss.on('connection', (conn, req) => {
	// Room name from URL path, e.g. /my-doc-name
	const docName = req.url.slice(1) || 'default';
	const entry = getDoc(docName);
	const { doc, awareness, conns } = entry;
	conns.add(conn);

	// Send initial sync step 1
	const encoder = encoding.createEncoder();
	encoding.writeVarUint(encoder, messageSync);
	syncProtocol.writeSyncStep1(encoder, doc);
	send(conn, encoding.toUint8Array(encoder));

	// Send current awareness states
	const awarenessStates = awareness.getStates();
	if (awarenessStates.size > 0) {
		const enc = encoding.createEncoder();
		encoding.writeVarUint(enc, messageAwareness);
		encoding.writeVarUint8Array(enc,
			awarenessProtocol.encodeAwarenessUpdate(awareness, Array.from(awarenessStates.keys()))
		);
		send(conn, encoding.toUint8Array(enc));
	}

	conn.on('message', (message) => {
		const buf = new Uint8Array(message);
		const decoder = decoding.createDecoder(buf);
		const messageType = decoding.readVarUint(decoder);

		switch (messageType) {
			case messageSync: {
				const encoder = encoding.createEncoder();
				encoding.writeVarUint(encoder, messageSync);
				syncProtocol.readSyncMessage(decoder, encoder, doc, conn);
				const reply = encoding.toUint8Array(encoder);
				if (encoding.length(encoder) > 1) {
					send(conn, reply);
				}
				// Broadcast update to other connections
				if (syncProtocol.messageYjsUpdate) {
					// Forward the raw update to all other clients
				}
				break;
			}
			case messageAwareness: {
				const update = decoding.readVarUint8Array(decoder);
				awarenessProtocol.applyAwarenessUpdate(awareness, update, conn);
				break;
			}
		}
	});

	// When doc updates, broadcast to all connections
	const updateHandler = (update, origin) => {
		const encoder = encoding.createEncoder();
		encoding.writeVarUint(encoder, messageSync);
		syncProtocol.writeUpdate(encoder, update);
		const message = encoding.toUint8Array(encoder);
		for (const c of conns) {
			if (c !== origin) send(c, message);
		}
	};
	doc.on('update', updateHandler);

	// When awareness changes, broadcast to all connections
	const awarenessHandler = ({ added, updated, removed }, origin) => {
		const changedClients = [...added, ...updated, ...removed];
		const encoder = encoding.createEncoder();
		encoding.writeVarUint(encoder, messageAwareness);
		encoding.writeVarUint8Array(encoder,
			awarenessProtocol.encodeAwarenessUpdate(awareness, changedClients)
		);
		const message = encoding.toUint8Array(encoder);
		for (const c of conns) {
			send(c, message);
		}
	};
	awareness.on('update', awarenessHandler);

	conn.on('close', () => {
		conns.delete(conn);
		awarenessProtocol.removeAwarenessStates(awareness, [doc.clientID], null);
		if (conns.size === 0) {
			doc.off('update', updateHandler);
			awareness.off('update', awarenessHandler);
			awareness.destroy();
			doc.destroy();
			docs.delete(docName);
		}
	});
});

server.listen(PORT, () => {
	console.log(`Y.js WebSocket server running on ws://localhost:${PORT}`);
});
