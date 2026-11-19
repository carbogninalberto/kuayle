export type BrowserGuide =
	| { app: 'Telegram' | 'Instagram' | 'Facebook'; ios: boolean }
	| 'home-screen'
	| null;

export function browserGuide(userAgent: string, installed: boolean, telegramBridge: boolean, touchPoints: number): BrowserGuide {
	if (installed) return null;
	const ios = /iPhone|iPad|iPod/i.test(userAgent) || (/Macintosh/.test(userAgent) && touchPoints > 1);
	if (telegramBridge || /Telegram/i.test(userAgent)) return { app: 'Telegram', ios };
	if (/Instagram/i.test(userAgent)) return { app: 'Instagram', ios };
	if (/FBAN|FBAV|FB_IAB|FB4A|FBIOS/i.test(userAgent)) return { app: 'Facebook', ios };
	if (ios && /Version\/[^ ]+.*Safari\//.test(userAgent)) return 'home-screen';
	return null;
}
