export const PUSH_SUBSCRIPTIONS_PATH = '/api/push/subscriptions';
export const PUSH_DEVICE_CHANGED = 'PUSH_DEVICE_CHANGED';

export type PushSubscriptionBody = {
	workspace_slug: string;
	endpoint: string;
	p256dh: string;
	auth: string;
	replace_id?: string;
};

export type EnrollPushDevice = (body: PushSubscriptionBody) => Promise<{ id: string }>;
export type RevokePushDevice = (id: string) => Promise<void>;

export function pushOptions(publicKey: string): PushSubscriptionOptionsInit {
	return {
		userVisibleOnly: true,
		applicationServerKey: Uint8Array.from(
			atob(publicKey.replace(/-/g, '+').replace(/_/g, '/')),
			(char) => char.charCodeAt(0)
		)
	};
}

export function pushSubscriptionBody(subscription: PushSubscription, workspaceSlug: string): PushSubscriptionBody {
	const { p256dh, auth } = subscription.toJSON().keys ?? {};
	if (!p256dh || !auth) throw new Error('Push subscription is missing its encryption keys');
	return { workspace_slug: workspaceSlug, endpoint: subscription.endpoint, p256dh, auth };
}

export async function subscribeToPush(
	manager: Pick<PushManager, 'getSubscription' | 'subscribe'>,
	publicKey: string,
	workspaceSlug: string,
	enroll: EnrollPushDevice,
	current?: PushSubscription | null
): Promise<string> {
	// Settings already checked for an existing subscription. With no
	// subscription, call subscribe immediately so Safari sees the tap.
	if (current === undefined) current = await manager.getSubscription();
	const subscription = current ?? (await manager.subscribe(pushOptions(publicKey)));
	return (await enroll(pushSubscriptionBody(subscription, workspaceSlug))).id;
}

export function needsPushHomeScreen(platform: { userAgent: string; maxTouchPoints: number; standalone?: boolean; standaloneDisplay: boolean }): boolean {
	const iOS = /iPad|iPhone|iPod/.test(platform.userAgent) ||
		(/Macintosh/.test(platform.userAgent) && platform.maxTouchPoints > 1);
	return iOS && !platform.standalone && !platform.standaloneDisplay;
}
