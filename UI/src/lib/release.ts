import packageJson from '../../package.json';

export const currentVersion = packageJson.version;
export const currentVersionLabel = `v${currentVersion}`;
export const currentReleaseUrl = `https://github.com/carbogninalberto/kuayle/releases/tag/${currentVersionLabel}`;
export const releasesManifestUrl = 'https://raw.githubusercontent.com/carbogninalberto/kuayle/main/UI/static/releases.json';
