import 'package:material_ui/material_ui.dart';

import '../models/asg_models.dart';
import '../services/asg_client.dart';
import '../services/host_storage.dart';
import '../theme/asg_theme.dart';

/// First screen: discovers ASG Desktop instances on the local network and lets
/// the user enter an IP address (with optional port) manually.
class ConnectScreen extends StatefulWidget {
  const ConnectScreen({
    super.key,
    required this.onConnected,
    this.onOpenSettings,
    this.initialError,
  });

  final void Function(HostInfo host) onConnected;
  final VoidCallback? onOpenSettings;
  final String? initialError;

  @override
  State<ConnectScreen> createState() => _ConnectScreenState();
}

class _ConnectScreenState extends State<ConnectScreen> {
  final _ipController = TextEditingController();
  final _focusNode = FocusNode();

  bool _scanning = false;
  bool _connecting = false;
  String? _error;
  List<HostInfo> _discovered = const [];
  List<String> _recent = const [];

  @override
  void initState() {
    super.initState();
    _error = widget.initialError;
    _init();
  }

  Future<void> _init() async {
    final recent = await HostStorage.loadRecentHosts();
    final last = await HostStorage.loadLastHost();
    if (!mounted) return;
    setState(() {
      _recent = recent;
      if (last != null && _ipController.text.isEmpty) {
        _ipController.text = last;
      }
    });
    _scanNetwork();
  }

  Future<void> _scanNetwork() async {
    if (_scanning) return;
    setState(() => _scanning = true);
    final found = await AsgClient.discoverHosts();
    if (!mounted) return;
    setState(() {
      _discovered = found;
      _scanning = false;
    });
  }

  Future<void> _connect(String rawInput) async {
    if (_connecting) return;
    setState(() {
      _connecting = true;
      _error = null;
    });
    try {
      final info = await AsgClient.probeHost(rawInput);
      await HostStorage.saveHost(info.displayAddress);
      if (!mounted) return;
      widget.onConnected(info);
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _connecting = false;
        _error = _friendlyError(e);
      });
    }
  }

  String _friendlyError(Object e) {
    final msg = e.toString();
    if (msg.contains('FormatException')) {
      return msg.replaceFirst('FormatException: ', '');
    }
    if (msg.contains('TimeoutException')) {
      return 'Timed out. Make sure ASG Desktop is open on that computer and both devices are on the same Wi-Fi.';
    }
    if (msg.contains('SocketException')) {
      return 'Could not reach that address. Check the IP, port (default 7789), and your computer\'s firewall.';
    }
    return msg.replaceFirst('HttpException: ', '');
  }

  @override
  void dispose() {
    _ipController.dispose();
    _focusNode.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final c = context.asg;
    return Scaffold(
      backgroundColor: c.bg,
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.symmetric(horizontal: 22, vertical: 28),
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 440),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  // Header + Crab Icon
                  Row(
                    children: [
                      ClipRRect(
                        borderRadius: BorderRadius.circular(14),
                        child: Image.asset(
                          'assets/icon/appicon.png',
                          width: 52,
                          height: 52,
                          filterQuality: FilterQuality.none,
                        ),
                      ),
                      const SizedBox(width: 14),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              'Connect to ASG',
                              style: TextStyle(
                                fontSize: 22,
                                fontWeight: FontWeight.w700,
                                color: c.text,
                                letterSpacing: -0.3,
                              ),
                            ),
                            const SizedBox(height: 2),
                            Text(
                              'Link to ASG running on your computer',
                              style: TextStyle(fontSize: 13.5, color: c.muted),
                            ),
                          ],
                        ),
                      ),
                      if (widget.onOpenSettings != null)
                        IconButton(
                          icon: Icon(Icons.settings_outlined, color: c.muted),
                          tooltip: 'Settings & updates',
                          onPressed: widget.onOpenSettings,
                        ),
                    ],
                  ),
                  const SizedBox(height: 26),

                  // Manual IP card
                  Container(
                    padding: const EdgeInsets.all(18),
                    decoration: BoxDecoration(
                      color: c.surface,
                      borderRadius: BorderRadius.circular(18),
                      border: Border.all(color: c.line),
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        Text(
                          'Computer IP address',
                          style: TextStyle(
                            fontSize: 13,
                            fontWeight: FontWeight.w600,
                            color: c.text2,
                          ),
                        ),
                        const SizedBox(height: 10),
                        TextField(
                          controller: _ipController,
                          focusNode: _focusNode,
                          enabled: !_connecting,
                          keyboardType: TextInputType.url,
                          textInputAction: TextInputAction.go,
                          onSubmitted: _connect,
                          style: TextStyle(
                            fontFamily: 'monospace',
                            fontSize: 15,
                            color: c.text,
                          ),
                          decoration: InputDecoration(
                            hintText: '192.168.1.42  (or host:7789)',
                            hintStyle: TextStyle(
                              fontFamily: 'monospace',
                              fontSize: 14,
                              color: c.muted,
                            ),
                            filled: true,
                            fillColor: c.raised,
                            contentPadding: const EdgeInsets.symmetric(
                              horizontal: 14,
                              vertical: 14,
                            ),
                            border: OutlineInputBorder(
                              borderRadius: BorderRadius.circular(12),
                              borderSide: BorderSide(color: c.line),
                            ),
                            enabledBorder: OutlineInputBorder(
                              borderRadius: BorderRadius.circular(12),
                              borderSide: BorderSide(color: c.line),
                            ),
                            focusedBorder: OutlineInputBorder(
                              borderRadius: BorderRadius.circular(12),
                              borderSide: BorderSide(
                                color: c.brand,
                                width: 1.5,
                              ),
                            ),
                          ),
                        ),
                        if (_error != null) ...[
                          const SizedBox(height: 12),
                          Container(
                            padding: const EdgeInsets.all(12),
                            decoration: BoxDecoration(
                              color: c.danger.withValues(alpha: 0.12),
                              borderRadius: BorderRadius.circular(10),
                              border: Border.all(
                                color: c.danger.withValues(alpha: 0.35),
                              ),
                            ),
                            child: Row(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Icon(
                                  Icons.error_outline_rounded,
                                  size: 18,
                                  color: c.danger,
                                ),
                                const SizedBox(width: 8),
                                Expanded(
                                  child: Text(
                                    _error!,
                                    style: TextStyle(
                                      fontSize: 12.5,
                                      color: c.text,
                                      height: 1.4,
                                    ),
                                  ),
                                ),
                              ],
                            ),
                          ),
                        ],
                        const SizedBox(height: 14),
                        FilledButton(
                          onPressed: _connecting
                              ? null
                              : () => _connect(_ipController.text),
                          style: FilledButton.styleFrom(
                            backgroundColor: c.brand,
                            foregroundColor: c.brandInk,
                            disabledBackgroundColor: c.brand.withValues(
                              alpha: 0.45,
                            ),
                            minimumSize: const Size.fromHeight(48),
                            shape: RoundedRectangleBorder(
                              borderRadius: BorderRadius.circular(12),
                            ),
                          ),
                          child: _connecting
                              ? SizedBox(
                                  width: 20,
                                  height: 20,
                                  child: CircularProgressIndicator(
                                    strokeWidth: 2.4,
                                    color: c.brandInk,
                                  ),
                                )
                              : const Text(
                                  'Connect',
                                  style: TextStyle(
                                    fontSize: 15,
                                    fontWeight: FontWeight.w700,
                                  ),
                                ),
                        ),
                      ],
                    ),
                  ),

                  const SizedBox(height: 22),

                  // Discovered on local network
                  Row(
                    children: [
                      Text(
                        'ON YOUR NETWORK',
                        style: TextStyle(
                          fontFamily: 'monospace',
                          fontSize: 11,
                          fontWeight: FontWeight.w600,
                          letterSpacing: 0.9,
                          color: c.muted,
                        ),
                      ),
                      const Spacer(),
                      TextButton.icon(
                        onPressed: _scanning ? null : _scanNetwork,
                        icon: _scanning
                            ? SizedBox(
                                width: 13,
                                height: 13,
                                child: CircularProgressIndicator(
                                  strokeWidth: 2,
                                  color: c.brand,
                                ),
                              )
                            : Icon(
                                Icons.radar_rounded,
                                size: 16,
                                color: c.brand,
                              ),
                        label: Text(
                          _scanning ? 'Scanning…' : 'Scan again',
                          style: TextStyle(fontSize: 12.5, color: c.text2),
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 6),
                  if (_discovered.isEmpty)
                    Container(
                      padding: const EdgeInsets.all(14),
                      decoration: BoxDecoration(
                        color: c.surface.withValues(alpha: 0.6),
                        borderRadius: BorderRadius.circular(14),
                        border: Border.all(color: c.line),
                      ),
                      child: Row(
                        children: [
                          Icon(
                            _scanning
                                ? Icons.wifi_tethering_rounded
                                : Icons.computer_rounded,
                            size: 20,
                            color: c.muted,
                          ),
                          const SizedBox(width: 12),
                          Expanded(
                            child: Text(
                              _scanning
                                  ? 'Looking for ASG Desktop on your Wi-Fi…'
                                  : 'No ASG Desktop found automatically. Open ASG Desktop on your computer or enter its IP above.',
                              style: TextStyle(
                                fontSize: 12.5,
                                color: c.muted,
                                height: 1.4,
                              ),
                            ),
                          ),
                        ],
                      ),
                    )
                  else
                    Column(
                      children: _discovered
                          .map((host) => _HostTile(
                                host: host,
                                enabled: !_connecting,
                                onTap: () {
                                  _ipController.text = host.displayAddress;
                                  _connect(host.baseUrl);
                                },
                              ))
                          .toList(),
                    ),

                  // Recent connections
                  if (_recent.isNotEmpty) ...[
                    const SizedBox(height: 22),
                    Text(
                      'RECENT',
                      style: TextStyle(
                        fontFamily: 'monospace',
                        fontSize: 11,
                        fontWeight: FontWeight.w600,
                        letterSpacing: 0.9,
                        color: c.muted,
                      ),
                    ),
                    const SizedBox(height: 8),
                    Wrap(
                      spacing: 8,
                      runSpacing: 8,
                      children: _recent
                          .map(
                            (addr) => InputChip(
                              label: Text(
                                addr,
                                style: TextStyle(
                                  fontFamily: 'monospace',
                                  fontSize: 12.5,
                                  color: c.text,
                                ),
                              ),
                              backgroundColor: c.surface,
                              side: BorderSide(color: c.line),
                              onPressed: _connecting
                                  ? null
                                  : () {
                                      _ipController.text = addr;
                                      _connect(addr);
                                    },
                              onDeleted: () async {
                                await HostStorage.removeRecent(addr);
                                final next = await HostStorage.loadRecentHosts();
                                if (mounted) setState(() => _recent = next);
                              },
                            ),
                          )
                          .toList(),
                    ),
                  ],

                  const SizedBox(height: 24),
                  Text(
                    'ASG Desktop listens on port ${AsgClient.defaultPort} automatically while open. Chats sync live in both directions.',
                    textAlign: TextAlign.center,
                    style: TextStyle(fontSize: 12, color: c.muted, height: 1.45),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class _HostTile extends StatelessWidget {
  const _HostTile({
    required this.host,
    required this.enabled,
    required this.onTap,
  });

  final HostInfo host;
  final bool enabled;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final c = context.asg;
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Material(
        color: c.surface,
        borderRadius: BorderRadius.circular(14),
        child: InkWell(
          onTap: enabled ? onTap : null,
          borderRadius: BorderRadius.circular(14),
          child: Container(
            padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
            decoration: BoxDecoration(
              borderRadius: BorderRadius.circular(14),
              border: Border.all(color: c.line),
            ),
            child: Row(
              children: [
                Container(
                  width: 36,
                  height: 36,
                  decoration: BoxDecoration(
                    color: c.brand.withValues(alpha: 0.14),
                    borderRadius: BorderRadius.circular(10),
                  ),
                  child: Icon(
                    Icons.desktop_windows_rounded,
                    size: 20,
                    color: c.brand,
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        host.hostname,
                        style: TextStyle(
                          fontSize: 14.5,
                          fontWeight: FontWeight.w600,
                          color: c.text,
                        ),
                      ),
                      const SizedBox(height: 2),
                      Text(
                        '${host.displayAddress} · ${host.modelLabel}',
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(
                          fontFamily: 'monospace',
                          fontSize: 11.5,
                          color: c.muted,
                        ),
                      ),
                    ],
                  ),
                ),
                Icon(Icons.chevron_right_rounded, color: c.muted),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
