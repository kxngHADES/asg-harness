# ASG Mobile

Flutter mobile client for ASG (Android & iOS). Connects to **ASG Desktop** running on your computer over your local network so models stay on your computer while chats, streaming replies, tool calls, and permission gates sync live in both directions.

## Connecting to your computer

1. Open **ASG Desktop** on your computer — it automatically starts its local network bridge on port `7789` (override with `ASG_MOBILE_ADDR`, or set `ASG_MOBILE_ADDR=off` to disable).
2. Make sure your phone and computer are on the same Wi-Fi network.
3. Open **ASG Mobile** — it scans your network automatically and also lets you type your computer's IP address (e.g. `192.168.1.42` or `192.168.1.42:7789`).

## In-app updates (Android, no Play Store)

Open **Settings → App Updates (GitHub)** to check `kxngHADES/asg-harness` for the latest release, download `asg-mobile_android.apk`, and launch Android's in-place installer without uninstalling.

For GitHub Actions releases to upgrade in place cleanly every time, Android requires every APK to be signed with the same keystore. Generate one once and add 4 repository secrets in GitHub:

```sh
keytool -genkey -v -keystore asg-release.jks -keyalg RSA -keysize 2048 -validity 10000 -alias asg
base64 -w 0 asg-release.jks   # paste into ANDROID_KEYSTORE_BASE64
```

Secrets: `ANDROID_KEYSTORE_BASE64`, `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS` (`asg`), `ANDROID_KEY_PASSWORD`.

## Development

```sh
cd mobile
flutter pub get
flutter analyze
flutter test
flutter run
```

## App icon

The launcher icons for Android (legacy + adaptive) and iOS (`AppIcon.appiconset`) as well as `assets/icon/appicon.png` are generated from `../desktop/build/appicon.png`.
