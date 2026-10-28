import { launchMachinePortWhenReady } from '$lib/api/dev-machines';

// Opens an app port in a new tab. The tab is opened synchronously to keep the
// user gesture; the launch URL carries a one-time ticket bound to that port's
// host, never a reusable credential.
export async function openAppPort(
	slug: string,
	machineId: string,
	portId: string,
	onStarting?: () => void
): Promise<void> {
	const popup = window.open('about:blank', '_blank');
	if (popup) popup.opener = null;
	try {
		const url = await launchMachinePortWhenReady(slug, machineId, portId, { onStarting });
		if (popup) popup.location.replace(url);
		else window.open(url, '_blank', 'noopener');
	} catch (error) {
		popup?.close();
		throw error;
	}
}
