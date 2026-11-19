// IndexedDB shares the enrollment with the service worker when all app windows are closed.
export type PushDevice = {
	id: string;
	userId: string;
	workspaceSlug: string;
	publicKey: string;
	needsReenable?: boolean;
};

export type PushDeviceStorage = {
	read(): Promise<PushDevice | undefined>;
	write(device: PushDevice): Promise<void>;
	take(): Promise<PushDevice | undefined>;
	replace(id: string | undefined, device: PushDevice): Promise<boolean>;
};

async function withDeviceStore<T>(mode: IDBTransactionMode, action: (store: IDBObjectStore) => IDBRequest<T>): Promise<T> {
	const db = await new Promise<IDBDatabase>((resolve, reject) => {
		const request = indexedDB.open('kuayle-push', 1);
		request.onupgradeneeded = () => request.result.createObjectStore('device');
		request.onsuccess = () => resolve(request.result);
		request.onerror = () => reject(request.error);
	});
	try {
		return await new Promise<T>((resolve, reject) => {
			const tx = db.transaction('device', mode);
			const request = action(tx.objectStore('device'));
			tx.oncomplete = () => resolve(request.result);
			tx.onabort = () => reject(tx.error ?? request.error);
		});
	} finally {
		db.close();
	}
}

export const pushDeviceStorage: PushDeviceStorage = {
	read: () => withDeviceStore('readonly', (store) => store.get('current')),
	async write(device) {
		await withDeviceStore('readwrite', (store) => store.put(device, 'current'));
	},
	take: () => withDeviceStore('readwrite', (store) => {
		const request = store.get('current');
		store.delete('current');
		return request;
	}),
	async replace(id, device) {
		let replaced = false;
		await withDeviceStore('readwrite', (store) => {
			const request = store.get('current') as IDBRequest<PushDevice | undefined>;
			request.onsuccess = () => {
				if (request.result?.id !== id) return;
				store.put(device, 'current');
				replaced = true;
			};
			return request;
		});
		return replaced;
	}
};
