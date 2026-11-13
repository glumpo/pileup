const fs = require('fs');
const path = require('path');
const { AndroidConfig, withAndroidManifest, withDangerousMod, withInfoPlist } = require('expo/config-plugins');

const networkSecurityConfig = `<?xml version="1.0" encoding="utf-8"?>
<network-security-config>
  <base-config cleartextTrafficPermitted="false" />
  <domain-config cleartextTrafficPermitted="true">
    <domain includeSubdomains="false">127.0.0.1</domain>
    <domain includeSubdomains="false">localhost</domain>
  </domain-config>
</network-security-config>
`;

module.exports = (config) => {
  config = AndroidConfig.Permissions.withPermissions(config, [
    'android.permission.ACCESS_NETWORK_STATE',
    'android.permission.CHANGE_WIFI_MULTICAST_STATE',
    'android.permission.READ_EXTERNAL_STORAGE',
    'android.permission.READ_MEDIA_IMAGES',
    'android.permission.READ_MEDIA_VIDEO',
    'android.permission.READ_MEDIA_VISUAL_USER_SELECTED',
  ]);
  config = withDangerousMod(config, [
    'android',
    (config) => {
      const dir = path.join(config.modRequest.platformProjectRoot, 'app/src/main/res/xml');
      fs.mkdirSync(dir, { recursive: true });
      fs.writeFileSync(path.join(dir, 'network_security_config.xml'), networkSecurityConfig);
      return config;
    },
  ]);
  config = withInfoPlist(config, (config) => {
    config.modResults.NSLocalNetworkUsageDescription =
      'Pileup ищет телефоны участников и телевизоры в этой Wi-Fi-сети, чтобы объединять фото и показывать их на экране.';
    config.modResults.NSPhotoLibraryUsageDescription =
      'Pileup добавляет фото и видео за выбранные даты в общую хронологию поездки. Участники видят их по Wi-Fi прямо с этого iPhone, без серверов.';
    config.modResults.NSBonjourServices = ['_pileup._tcp', '_googlecast._tcp', '_airplay._tcp'];
    const scenes = (config.modResults.UIApplicationSceneManifest ??= {});
    scenes.UISceneConfigurations = {
      ...scenes.UISceneConfigurations,
      UIWindowSceneSessionRoleExternalDisplayNonInteractive: [
        { UISceneConfigurationName: 'Presentation', UISceneDelegateClassName: 'PileupPresentationSceneDelegate' },
      ],
    };
    return config;
  });
  return withAndroidManifest(config, (config) => {
    const application = AndroidConfig.Manifest.getMainApplicationOrThrow(config.modResults);
    application.$['android:networkSecurityConfig'] = '@xml/network_security_config';
    return config;
  });
};
