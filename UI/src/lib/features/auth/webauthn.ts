// Browser side of the passkey ceremonies: the server speaks JSON with
// base64url strings, navigator.credentials speaks ArrayBuffers.

/** Options as the backend sends them (WebAuthn JSON form). */
export interface PasskeyOptionsJSON {
	publicKey: Record<string, any>;
	mediation?: string;
}

type CredentialsHost = { PublicKeyCredential?: unknown; navigator?: { credentials?: unknown } };

/** Whether this browser can create and use passkeys at all. */
export function passkeysSupported(host: CredentialsHost = globalThis as CredentialsHost): boolean {
	return typeof host.PublicKeyCredential === 'function' && !!host.navigator?.credentials;
}

/** Whether passkeys can be offered in the email field's autofill. */
export async function conditionalMediationAvailable(
	host: { PublicKeyCredential?: any } = globalThis as any
): Promise<boolean> {
	try {
		return (await host.PublicKeyCredential?.isConditionalMediationAvailable?.()) === true;
	} catch {
		return false;
	}
}

export function base64urlToBuffer(value: string): ArrayBuffer {
	const base64 = value.replace(/-/g, '+').replace(/_/g, '/');
	const binary = atob(base64.padEnd(Math.ceil(base64.length / 4) * 4, '='));
	const bytes = new Uint8Array(binary.length);
	for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
	return bytes.buffer;
}

export function bufferToBase64url(buffer: ArrayBuffer | ArrayBufferView): string {
	const bytes = ArrayBuffer.isView(buffer)
		? new Uint8Array(buffer.buffer, buffer.byteOffset, buffer.byteLength)
		: new Uint8Array(buffer);
	let binary = '';
	for (const byte of bytes) binary += String.fromCharCode(byte);
	return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

function descriptors(list: any[] | undefined) {
	return list?.map((item) => ({ ...item, id: base64urlToBuffer(item.id) }));
}

/** Options for navigator.credentials.create from the server's JSON. */
export function toCreationOptions(options: PasskeyOptionsJSON): CredentialCreationOptions {
	const publicKey = options.publicKey;
	return {
		publicKey: {
			...publicKey,
			challenge: base64urlToBuffer(publicKey.challenge),
			user: { ...publicKey.user, id: base64urlToBuffer(publicKey.user.id) },
			excludeCredentials: descriptors(publicKey.excludeCredentials)
		} as PublicKeyCredentialCreationOptions
	};
}

/** Options for navigator.credentials.get from the server's JSON. */
export function toRequestOptions(options: PasskeyOptionsJSON): CredentialRequestOptions {
	const publicKey = options.publicKey;
	return {
		publicKey: {
			...publicKey,
			challenge: base64urlToBuffer(publicKey.challenge),
			allowCredentials: descriptors(publicKey.allowCredentials)
		} as PublicKeyCredentialRequestOptions
	};
}

/** The JSON form of a credential the server parses, for both ceremonies. */
export function credentialToJSON(credential: any): Record<string, unknown> {
	const response = credential.response;
	const out: Record<string, unknown> = {
		clientDataJSON: bufferToBase64url(response.clientDataJSON)
	};
	if (response.attestationObject) {
		out.attestationObject = bufferToBase64url(response.attestationObject);
		out.transports = typeof response.getTransports === 'function' ? response.getTransports() : [];
	} else {
		out.authenticatorData = bufferToBase64url(response.authenticatorData);
		out.signature = bufferToBase64url(response.signature);
		// Some authenticators return no user handle; the server then finds the
		// account by the credential ID alone.
		if (response.userHandle && response.userHandle.byteLength > 0) {
			out.userHandle = bufferToBase64url(response.userHandle);
		}
	}
	return {
		id: credential.id,
		rawId: bufferToBase64url(credential.rawId),
		type: credential.type,
		authenticatorAttachment: credential.authenticatorAttachment ?? undefined,
		response: out,
		clientExtensionResults:
			typeof credential.getClientExtensionResults === 'function' ? credential.getClientExtensionResults() : {}
	};
}

/** The user closed the browser's passkey sheet, or a newer request replaced this one. */
export function isPasskeyCancellation(error: unknown): boolean {
	const name = (error as { name?: string } | null)?.name;
	return name === 'NotAllowedError' || name === 'AbortError';
}

/** The authenticator already holds a passkey for this account. */
export function isPasskeyDuplicate(error: unknown): boolean {
	return (error as { name?: string } | null)?.name === 'InvalidStateError';
}

/** The server's error code of a failed API call, if any. */
export function apiErrorCode(error: unknown): string {
	const code = (error as { error?: { code?: unknown } } | null)?.error?.code;
	return typeof code === 'string' ? code : '';
}

/** A default name for a new passkey, e.g. "Chrome on macOS"; empty when unknown. */
export function suggestPasskeyName(userAgent: string): { browser: string; os: string } | null {
	const browser = /Edg\//.test(userAgent)
		? 'Edge'
		: /OPR\//.test(userAgent)
			? 'Opera'
			: /Firefox\//.test(userAgent)
				? 'Firefox'
				: /Chrome\//.test(userAgent)
					? 'Chrome'
					: /Safari\//.test(userAgent)
						? 'Safari'
						: '';
	const os = /iPhone|iPad|iPod/.test(userAgent)
		? 'iOS'
		: /Android/.test(userAgent)
			? 'Android'
			: /Mac OS X|Macintosh/.test(userAgent)
				? 'macOS'
				: /Windows/.test(userAgent)
					? 'Windows'
					: /CrOS/.test(userAgent)
						? 'ChromeOS'
						: /Linux/.test(userAgent)
							? 'Linux'
							: '';
	return browser && os ? { browser, os } : null;
}
