import 'dart:io';

import 'package:material_ui/material_ui.dart';

import '../models/asg_models.dart';
import '../services/app_updater.dart';
import '../services/asg_client.dart';
import '../theme/asg_theme.dart';
import '../widgets/model_picker_sheet.dart';

class SettingsScreen extends StatefulWidget {
  const SettingsScreen({
    super.key,
    required this.themeMode,
    required this.onThemeModeChanged,
    this.host,
    this.client,
    this.model,
    this.onModelChanged,
    this.onDisconnect,
  });

  final ThemeMode themeMode;
  final void Function(ThemeMode mode) onThemeModeChanged;
  final HostInfo? host;
  final AsgClient? client;
  final ModelInfo? model;
  final void Function(ModelInfo updated)? onModelChanged;
  final VoidCallback? onDisconnect;

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  late ThemeMode _themeMode;
  ModelInfo? _model;

  AppVersion? _installed;
  ReleaseInfo? _latest;
  bool _checking = false;
  bool _downloading = false;
  double? _downloadProgress;
  int _receivedBytes = 0;
  int _totalBytes = 0;
  String? _downloadedPath;
  String? _updateMessage;
  String? _updateError;

  @override
  void initState() {
    super.initState();
    _themeMode = widget.themeMode;
    _model = widget.model;
    _loadVersion();
  }

  Future<void> _loadVersion() async {
    final v = await AppUpdater.getInstalledVersion();
    if (mounted) setState(() => _installed = v);
  }

  Future<void> _checkUpdates() async {
    if (_checking) return;
    setState(() {
      _checking = true;
      _updateError = null;
      _updateMessage = null;
    });
    try {
      final rel = await AppUpdater.fetchLatestRelease();
      final cur = _installed ?? await AppUpdater.getInstalledVersion();
      if (!mounted) return;
      final cmp = AppUpdater.compareSemver(rel.cleanVersion, cur.version);
      setState(() {
        _installed = cur;
        _latest = rel;
        _checking = false;
        if (cmp <= 0) {
          _updateMessage = 'You are on the latest version (${rel.tag}).';
        }
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _checking = false;
        _updateError = e.toString().replaceFirst('HttpException: ', '');
      });
    }
  }

  Future<void> _downloadAndInstall() async {
    final rel = _latest;
    if (rel == null || _downloading) return;

    // If we already downloaded the APK (e.g. user just enabled unknown-sources
    // permission), jump straight to installing without re-downloading.
    if (_downloadedPath != null && File(_downloadedPath!).existsSync()) {
      await _triggerInstall(_downloadedPath!);
      return;
    }

    setState(() {
      _downloading = true;
      _downloadProgress = null;
      _receivedBytes = 0;
      _totalBytes = rel.apkSize;
      _updateError = null;
      _updateMessage = null;
    });
    try {
      final path = await AppUpdater.downloadApk(
        rel,
        onProgress: (p, received, total) {
          if (!mounted) return;
          setState(() {
            _downloadProgress = p;
            _receivedBytes = received;
            _totalBytes = total;
          });
        },
      );
      if (!mounted) return;
      setState(() {
        _downloading = false;
        _downloadedPath = path;
      });
      await _triggerInstall(path);
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _downloading = false;
        _updateError = e.toString().replaceFirst('HttpException: ', '');
      });
    }
  }

  Future<void> _triggerInstall(String path) async {
    try {
      final res = await AppUpdater.installApk(path);
      if (!mounted) return;
      setState(() {
        switch (res) {
          case ApkInstallResult.started:
            _updateMessage =
                'Installer opened. Confirm "Update" to upgrade in place without losing your settings.';
          case ApkInstallResult.needsPermission:
            _updateMessage =
                'Enable "Allow from this source" for ASG in the screen that opened, then tap "Install update" again.';
          case ApkInstallResult.unsupportedPlatform:
            _updateMessage =
                'In-app APK updates are supported on Android. Download the latest IPA from GitHub Releases on iOS.';
        }
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _updateError = 'Could not launch installer: $e';
      });
    }
  }

  Future<void> _openModelPicker() async {
    final client = widget.client;
    if (client == null) return;
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      backgroundColor: Colors.transparent,
      builder: (_) => ModelPickerSheet(
        client: client,
        current: _model,
        onChanged: (updated) {
          setState(() => _model = updated);
          widget.onModelChanged?.call(updated);
        },
      ),
    );
  }

  String _fmtBytes(int b) {
    if (b <= 0) return '0 B';
    if (b < 1024 * 1024) return '${(b / 1024).toStringAsFixed(0)} KB';
    return '${(b / (1024 * 1024)).toStringAsFixed(1)} MB';
  }

  @override
  Widget build(BuildContext context) {
    final c = context.asg;
    final hasUpdate = _latest != null &&
        _installed != null &&
        AppUpdater.compareSemver(_latest!.cleanVersion, _installed!.version) > 0;

    return Scaffold(
      backgroundColor: c.bg,
      appBar: AppBar(title: const Text('Settings')),
      body: ListView(
        padding: const EdgeInsets.fromLTRB(16, 8, 16, 36),
        children: [
          if (widget.host != null) ...[
            _sectionLabel(c, 'CONNECTION'),
            _card(
              c,
              child: Column(
                children: [
                  ListTile(
                    leading: Icon(Icons.computer_rounded, color: c.brand),
                    title: Text(
                      widget.host!.hostname,
                      style: TextStyle(
                        fontWeight: FontWeight.w600,
                        color: c.text,
                      ),
                    ),
                    subtitle: Text(
                      widget.host!.displayAddress,
                      style: TextStyle(
                        fontFamily: 'monospace',
                        fontSize: 12,
                        color: c.muted,
                      ),
                    ),
                    trailing: TextButton(
                      onPressed: () {
                        Navigator.of(context).pop();
                        widget.onDisconnect?.call();
                      },
                      child: Text(
                        'Switch',
                        style: TextStyle(color: c.danger),
                      ),
                    ),
                  ),
                  if (widget.client != null) ...[
                    Divider(height: 1, color: c.line),
                    ListTile(
                      leading: Icon(Icons.auto_awesome_rounded, color: c.kindCode),
                      title: Text(
                        'Active model',
                        style: TextStyle(color: c.text),
                      ),
                      subtitle: Text(
                        _model != null
                            ? '${_model!.provider} · ${_model!.label}'
                            : widget.host!.modelLabel,
                        style: TextStyle(
                          fontFamily: 'monospace',
                          fontSize: 12,
                          color: c.muted,
                        ),
                      ),
                      trailing: Icon(Icons.chevron_right_rounded, color: c.muted),
                      onTap: _openModelPicker,
                    ),
                  ],
                ],
              ),
            ),
            const SizedBox(height: 20),
          ],

          _sectionLabel(c, 'APPEARANCE'),
          _card(
            c,
            child: Padding(
              padding: const EdgeInsets.all(14),
              child: SegmentedButton<ThemeMode>(
                segments: const [
                  ButtonSegment(
                    value: ThemeMode.system,
                    label: Text('System'),
                    icon: Icon(Icons.brightness_auto_rounded, size: 17),
                  ),
                  ButtonSegment(
                    value: ThemeMode.dark,
                    label: Text('Dark'),
                    icon: Icon(Icons.dark_mode_rounded, size: 17),
                  ),
                  ButtonSegment(
                    value: ThemeMode.light,
                    label: Text('Light'),
                    icon: Icon(Icons.light_mode_rounded, size: 17),
                  ),
                ],
                selected: {_themeMode},
                onSelectionChanged: (sel) {
                  final next = sel.first;
                  setState(() => _themeMode = next);
                  widget.onThemeModeChanged(next);
                },
              ),
            ),
          ),

          const SizedBox(height: 20),
          _sectionLabel(c, 'APP UPDATES (GITHUB)'),
          _card(
            c,
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Row(
                    children: [
                      ClipRRect(
                        borderRadius: BorderRadius.circular(10),
                        child: Image.asset(
                          'assets/icon/appicon.png',
                          width: 40,
                          height: 40,
                          filterQuality: FilterQuality.none,
                        ),
                      ),
                      const SizedBox(width: 12),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              'ASG Mobile',
                              style: TextStyle(
                                fontSize: 15,
                                fontWeight: FontWeight.w700,
                                color: c.text,
                              ),
                            ),
                            Text(
                              'Installed: ${_installed?.display ?? '…'} · ${AppUpdater.repo}',
                              style: TextStyle(
                                fontFamily: 'monospace',
                                fontSize: 11.5,
                                color: c.muted,
                              ),
                            ),
                          ],
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 14),
                  OutlinedButton.icon(
                    onPressed: (_checking || _downloading) ? null : _checkUpdates,
                    icon: _checking
                        ? SizedBox(
                            width: 15,
                            height: 15,
                            child: CircularProgressIndicator(
                              strokeWidth: 2,
                              color: c.brand,
                            ),
                          )
                        : const Icon(Icons.system_update_rounded, size: 18),
                    label: Text(
                      _checking ? 'Checking GitHub…' : 'Check for updates',
                    ),
                  ),
                  if (_updateError != null) ...[
                    const SizedBox(height: 12),
                    Container(
                      padding: const EdgeInsets.all(10),
                      decoration: BoxDecoration(
                        color: c.danger.withValues(alpha: 0.12),
                        borderRadius: BorderRadius.circular(10),
                      ),
                      child: Text(
                        _updateError!,
                        style: TextStyle(fontSize: 12.5, color: c.danger),
                      ),
                    ),
                  ],
                  if (_updateMessage != null) ...[
                    const SizedBox(height: 12),
                    Container(
                      padding: const EdgeInsets.all(10),
                      decoration: BoxDecoration(
                        color: c.ok.withValues(alpha: 0.12),
                        borderRadius: BorderRadius.circular(10),
                      ),
                      child: Text(
                        _updateMessage!,
                        style: TextStyle(fontSize: 12.5, color: c.text),
                      ),
                    ),
                  ],
                  if (hasUpdate && _latest != null) ...[
                    const SizedBox(height: 14),
                    Divider(height: 1, color: c.line),
                    const SizedBox(height: 14),
                    Row(
                      children: [
                        Container(
                          padding: const EdgeInsets.symmetric(
                            horizontal: 8,
                            vertical: 4,
                          ),
                          decoration: BoxDecoration(
                            color: c.brand,
                            borderRadius: BorderRadius.circular(99),
                          ),
                          child: Text(
                            'NEW ${_latest!.tag}',
                            style: TextStyle(
                              fontFamily: 'monospace',
                              fontSize: 11,
                              fontWeight: FontWeight.w700,
                              color: c.brandInk,
                            ),
                          ),
                        ),
                        const SizedBox(width: 8),
                        if (_latest!.sizeLabel.isNotEmpty)
                          Text(
                            _latest!.sizeLabel,
                            style: TextStyle(
                              fontFamily: 'monospace',
                              fontSize: 11.5,
                              color: c.muted,
                            ),
                          ),
                      ],
                    ),
                    if (_latest!.notes.trim().isNotEmpty) ...[
                      const SizedBox(height: 10),
                      Container(
                        constraints: const BoxConstraints(maxHeight: 160),
                        padding: const EdgeInsets.all(10),
                        decoration: BoxDecoration(
                          color: c.raised,
                          borderRadius: BorderRadius.circular(10),
                        ),
                        child: SingleChildScrollView(
                          child: Text(
                            _latest!.notes.trim(),
                            style: TextStyle(
                              fontSize: 12.5,
                              color: c.text2,
                              height: 1.45,
                            ),
                          ),
                        ),
                      ),
                    ],
                    if (_downloading) ...[
                      const SizedBox(height: 12),
                      LinearProgressIndicator(
                        value: _downloadProgress,
                        color: c.brand,
                        backgroundColor: c.raised,
                      ),
                      const SizedBox(height: 6),
                      Text(
                        _totalBytes > 0
                            ? 'Downloading ${_fmtBytes(_receivedBytes)} / ${_fmtBytes(_totalBytes)}…'
                            : 'Downloading ${_fmtBytes(_receivedBytes)}…',
                        style: TextStyle(
                          fontFamily: 'monospace',
                          fontSize: 11.5,
                          color: c.muted,
                        ),
                      ),
                    ],
                    const SizedBox(height: 12),
                    FilledButton.icon(
                      onPressed: _downloading ? null : _downloadAndInstall,
                      style: FilledButton.styleFrom(
                        backgroundColor: c.brand,
                        foregroundColor: c.brandInk,
                        minimumSize: const Size.fromHeight(46),
                      ),
                      icon: const Icon(Icons.download_rounded, size: 19),
                      label: Text(
                        _downloadedPath != null
                            ? 'Install update'
                            : 'Download & update in place',
                        style: const TextStyle(fontWeight: FontWeight.w700),
                      ),
                    ),
                  ],
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _sectionLabel(AsgColors c, String label) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(4, 0, 4, 8),
      child: Text(
        label,
        style: TextStyle(
          fontFamily: 'monospace',
          fontSize: 11,
          fontWeight: FontWeight.w600,
          letterSpacing: 0.9,
          color: c.muted,
        ),
      ),
    );
  }

  Widget _card(AsgColors c, {required Widget child}) {
    return Container(
      decoration: BoxDecoration(
        color: c.surface,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: c.line),
      ),
      child: child,
    );
  }
}
