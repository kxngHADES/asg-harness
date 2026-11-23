import 'dart:convert';
import 'dart:io';

import 'package:flutter/services.dart';

class AppVersion {
  const AppVersion({required this.version, required this.buildNumber});

  final String version;
  final String buildNumber;

  String get display =>
      buildNumber.isNotEmpty ? 'v$version ($buildNumber)' : 'v$version';
}

class ReleaseInfo {
  const ReleaseInfo({
    required this.tag,
    required this.notes,
    required this.htmlUrl,
    required this.publishedAt,
    required this.apkUrl,
    required this.apkSize,
  });

  final String tag;
  final String notes;
  final String htmlUrl;
  final DateTime? publishedAt;
  final String? apkUrl;
  final int apkSize;

  String get cleanVersion => tag.startsWith('v') ? tag.substring(1) : tag;

  String get sizeLabel {
    if (apkSize <= 0) return '';
    if (apkSize < 1024 * 1024) {
      return '${(apkSize / 1024).toStringAsFixed(0)} KB';
    }
    return '${(apkSize / (1024 * 1024)).toStringAsFixed(1)} MB';
  }
}

enum ApkInstallResult { started, needsPermission, unsupportedPlatform }

/// Checks GitHub Releases for newer Android APKs and triggers Android's native
/// in-place package installer without needing the Play Store.
class AppUpdater {
  static const String repo = 'kxngHADES/asg-harness';
  static const String apkAssetName = 'asg-mobile_android.apk';
  static const MethodChannel _channel = MethodChannel(
    'com.asg.asg_harness/updater',
  );

  static Future<AppVersion> getInstalledVersion() async {
    try {
      final map = await _channel.invokeMapMethod<String, dynamic>(
        'getAppVersion',
      );
      if (map != null) {
        return AppVersion(
          version: (map['version'] as String?) ?? '0.1.0',
          buildNumber: (map['buildNumber'] as String?) ?? '',
        );
      }
    } catch (_) {}
    return const AppVersion(version: '0.1.0', buildNumber: '1');
  }

  /// Compares two semantic version strings (ignoring leading "v" and
  /// pre-release/build suffixes). Returns > 0 if [a] is newer than [b].
  static int compareSemver(String a, String b) {
    List<int> parse(String s) {
      final clean = s.trim().replaceFirst(RegExp(r'^[vV]'), '').split(RegExp(r'[-+]')).first;
      final parts = clean.split('.');
      return List<int>.generate(
        3,
        (i) => i < parts.length ? (int.tryParse(parts[i]) ?? 0) : 0,
      );
    }

    final pa = parse(a);
    final pb = parse(b);
    for (var i = 0; i < 3; i++) {
      if (pa[i] != pb[i]) return pa[i].compareTo(pb[i]);
    }
    return 0;
  }

  static Future<ReleaseInfo> fetchLatestRelease() async {
    final client = HttpClient()..connectionTimeout = const Duration(seconds: 10);
    try {
      final req = await client.getUrl(
        Uri.parse('https://api.github.com/repos/$repo/releases/latest'),
      );
      req.headers.set(HttpHeaders.acceptHeader, 'application/vnd.github+json');
      req.headers.set(HttpHeaders.userAgentHeader, 'asg-mobile');
      final resp = await req.close();
      final body = await resp.transform(utf8.decoder).join();
      if (resp.statusCode != 200) {
        throw HttpException('GitHub returned HTTP ${resp.statusCode}');
      }
      final j = jsonDecode(body) as Map<String, dynamic>;
      final assets = (j['assets'] as List<dynamic>?) ?? const [];
      String? apkUrl;
      int apkSize = 0;
      for (final item in assets.whereType<Map<String, dynamic>>()) {
        if (item['name'] == apkAssetName) {
          apkUrl = item['browser_download_url'] as String?;
          apkSize = (item['size'] as int?) ?? 0;
          break;
        }
      }
      return ReleaseInfo(
        tag: (j['tag_name'] as String?) ?? '',
        notes: (j['body'] as String?) ?? '',
        htmlUrl: (j['html_url'] as String?) ??
            'https://github.com/$repo/releases/latest',
        publishedAt: DateTime.tryParse((j['published_at'] as String?) ?? ''),
        apkUrl: apkUrl,
        apkSize: apkSize,
      );
    } finally {
      client.close(force: true);
    }
  }

  /// Downloads the APK into `<cacheDir>/updates/asg-mobile_android.apk`.
  static Future<String> downloadApk(
    ReleaseInfo release, {
    void Function(double? progress, int received, int total)? onProgress,
  }) async {
    final url = release.apkUrl;
    if (url == null || url.isEmpty) {
      throw const HttpException(
        'This release does not include $apkAssetName yet.',
      );
    }
    final cacheDir = await _channel.invokeMethod<String>('getCacheDir');
    if (cacheDir == null || cacheDir.isEmpty) {
      throw StateError('Could not resolve app cache directory');
    }
    final dir = Directory('$cacheDir/updates');
    if (!dir.existsSync()) {
      dir.createSync(recursive: true);
    }
    final file = File('${dir.path}/$apkAssetName');
    final tmp = File('${dir.path}/$apkAssetName.part');

    final client = HttpClient()..connectionTimeout = const Duration(seconds: 15);
    try {
      final req = await client.getUrl(Uri.parse(url));
      req.headers.set(HttpHeaders.userAgentHeader, 'asg-mobile');
      final resp = await req.close();
      if (resp.statusCode != 200) {
        throw HttpException('Download failed (HTTP ${resp.statusCode})');
      }
      final total =
          resp.contentLength > 0 ? resp.contentLength : release.apkSize;
      var received = 0;
      final sink = tmp.openWrite();
      try {
        await for (final chunk in resp) {
          sink.add(chunk);
          received += chunk.length;
          onProgress?.call(
            total > 0 ? (received / total).clamp(0.0, 1.0) : null,
            received,
            total,
          );
        }
        await sink.flush();
      } finally {
        await sink.close();
      }
      if (file.existsSync()) file.deleteSync();
      tmp.renameSync(file.path);
      return file.path;
    } finally {
      client.close(force: true);
    }
  }

  static Future<ApkInstallResult> installApk(String path) async {
    if (!Platform.isAndroid) {
      return ApkInstallResult.unsupportedPlatform;
    }
    final res = await _channel.invokeMethod<String>('installApk', {
      'path': path,
    });
    if (res == 'needs_permission') {
      return ApkInstallResult.needsPermission;
    }
    return ApkInstallResult.started;
  }
}
