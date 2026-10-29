// Guest edition: same shell and core, its own app id, install folder, data
// folder (%APPDATA%\cmnet guest) and update channel, so it installs beside the
// host app and never touches its data.
const base = require('./package.json').build;

module.exports = {
  ...base,
  appId: 'com.twipz333.cmnet.guest',
  productName: 'cmnet guest',
  directories: { output: 'dist-guest' },
  extraMetadata: { name: 'cmnet-guest', productName: 'cmnet guest', cmnetEdition: 'guest' },
  nsis: {
    ...base.nsis,
    shortcutName: 'cmnet guest',
    artifactName: 'cmnet-guest-setup-${version}.${ext}',
  },
  publish: { ...base.publish, channel: 'guest' },
};
