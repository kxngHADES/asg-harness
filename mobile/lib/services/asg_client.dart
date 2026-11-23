import 'dart:async';
import 'dart:convert';
import 'dart:io';

import '../models/asg_models.dart';

enum ConnectionStateStatus { connecting, connected, reconnecting, disconnected }

/// HTTP JSON + SSE + UDP discovery client for ASG Desktop's network bridge.
class AsgClient {
  AsgClient({required this.baseUrl}) : _http = HttpClient() {
    _http.connectionTimeout = const Duration(seconds: 6);
  }

  static const int defaultPort = 7789;
  static const String _discoverProbe = 'ASG_DISCOVER';

  final String baseUrl;
  final HttpClient _http;

  final _eventController = StreamController<AsgEvent>.broadcast();
  final _statusController = StreamController<ConnectionStateStatus>.broadcast();

  Stream<AsgEvent> get events => _eventController.stream;
  Stream<ConnectionStateStatus> get statusStream => _statusController.stream;
  ConnectionStateStatus _status = ConnectionStateStatus.disconnected;
  ConnectionStateStatus get status => _status;

  bool _disposed = false;
  HttpClient? _sseClient;

  /// Normalizes user input such as "192.168.1.42", "192.168.1.42:7789",
  /// or "http://my-pc.local:7789/" into "http://host:port".
  static String normalizeBaseUrl(String raw) {
    var s = raw.trim();
    if (s.isEmpty) {
      throw const FormatException('Enter an IP address or hostname');
    }
    if (!s.startsWith('http://') && !s.startsWith('https://')) {
      s = 'http://$s';
    }
    final uri = Uri.tryParse(s);
    if (uri == null || uri.host.isEmpty) {
      throw const FormatException('Invalid IP address or hostname');
    }
    final port = uri.hasPort ? uri.port : defaultPort;
    return '${uri.scheme}://${uri.host}:$port';
  }

  /// Queries GET /api/info on a candidate host to verify ASG Desktop is running.
  static Future<HostInfo> probeHost(
    String rawInput, {
    Duration timeout = const Duration(seconds: 5),
  }) async {
    final base = normalizeBaseUrl(rawInput);
    final client = HttpClient()..connectionTimeout = timeout;
    try {
      final req = await client
          .getUrl(Uri.parse('$base/api/info'))
          .timeout(timeout);
      final resp = await req.close().timeout(timeout);
      final body = await resp.transform(utf8.decoder).join().timeout(timeout);
      if (resp.statusCode != 200) {
        throw HttpException('Server returned HTTP ${resp.statusCode}');
      }
      final map = jsonDecode(body) as Map<String, dynamic>;
      if (map['app'] != 'asg-desktop') {
        throw const FormatException('That server is not ASG Desktop');
      }
      return HostInfo.fromJson(base, map);
    } finally {
      client.close(force: true);
    }
  }

  /// Broadcasts a UDP discovery packet on port 7789 and collects responding
  /// ASG Desktop computers on the local network.
  static Future<List<HostInfo>> discoverHosts({
    Duration timeout = const Duration(seconds: 2),
  }) async {
    final found = <String, HostInfo>{};
    RawDatagramSocket? socket;
    try {
      socket = await RawDatagramSocket.bind(InternetAddress.anyIPv4, 0);
      socket.broadcastEnabled = true;

      final done = Completer<void>();
      final sub = socket.listen((event) {
        if (event != RawSocketEvent.read) return;
        final dg = socket?.receive();
        if (dg == null) return;
        try {
          final text = utf8.decode(dg.data);
          final map = jsonDecode(text) as Map<String, dynamic>;
          if (map['app'] != 'asg-desktop') return;
          final port = (map['port'] as int?) ?? defaultPort;
          final base = 'http://${dg.address.address}:$port';
          found[base] = HostInfo.fromJson(base, map);
        } catch (_) {
          // Ignore malformed replies
        }
      }, onError: (Object _) {
        // Send failures (e.g. "No route to host" with no network) arrive here
        // rather than being thrown by send(); ignore them so manual IP entry
        // still works.
      });

      final payload = utf8.encode(_discoverProbe);
      socket.send(payload, InternetAddress('255.255.255.255'), defaultPort);
      // Send a second probe after 400ms for reliability over Wi-Fi.
      Timer(const Duration(milliseconds: 400), () {
        try {
          socket?.send(payload, InternetAddress('255.255.255.255'), defaultPort);
        } catch (_) {}
      });

      Timer(timeout, () {
        if (!done.isCompleted) done.complete();
      });
      await done.future;
      await sub.cancel();
    } catch (_) {
      // Discovery may fail on restricted networks; manual IP entry still works.
    } finally {
      socket?.close();
    }
    return found.values.toList();
  }

  void _setStatus(ConnectionStateStatus next) {
    if (_status == next || _disposed) return;
    _status = next;
    _statusController.add(next);
  }

  /// Starts the persistent SSE event loop with automatic reconnect.
  void startEvents() {
    if (_disposed) return;
    _runEventLoop();
  }

  Future<void> _runEventLoop() async {
    var attempt = 0;
    while (!_disposed) {
      _setStatus(
        attempt == 0
            ? ConnectionStateStatus.connecting
            : ConnectionStateStatus.reconnecting,
      );
      final client = HttpClient()
        ..connectionTimeout = const Duration(seconds: 6);
      _sseClient = client;
      try {
        final req = await client.getUrl(Uri.parse('$baseUrl/api/events'));
        req.headers.set(HttpHeaders.acceptHeader, 'text/event-stream');
        final resp = await req.close();
        if (resp.statusCode != 200) {
          throw HttpException('SSE HTTP ${resp.statusCode}');
        }
        attempt = 0;
        _setStatus(ConnectionStateStatus.connected);

        final lines = resp
            .transform(utf8.decoder)
            .transform(const LineSplitter());
        await for (final line in lines) {
          if (_disposed) break;
          if (!line.startsWith('data: ')) continue;
          final raw = line.substring(6).trim();
          if (raw.isEmpty) continue;
          try {
            final map = jsonDecode(raw) as Map<String, dynamic>;
            final name = map['name'] as String?;
            if (name != null) {
              _eventController.add(AsgEvent(name: name, data: map['data']));
            }
          } catch (_) {}
        }
      } catch (_) {
        // Reconnect below.
      } finally {
        client.close(force: true);
        if (_sseClient == client) _sseClient = null;
      }
      if (_disposed) break;
      attempt++;
      final waitSec = attempt < 4 ? attempt : 4;
      await Future<void>.delayed(Duration(seconds: waitSec));
    }
    _setStatus(ConnectionStateStatus.disconnected);
  }

  Future<dynamic> call(String method, [List<dynamic> args = const []]) async {
    final req = await _http.postUrl(Uri.parse('$baseUrl/api/call/$method'));
    req.headers.contentType = ContentType.json;
    req.write(jsonEncode(args));
    final resp = await req.close();
    final body = await resp.transform(utf8.decoder).join();
    if (resp.statusCode != 200) {
      throw HttpException(
        body.trim().isNotEmpty ? body.trim() : 'HTTP ${resp.statusCode}',
      );
    }
    if (body.trim().isEmpty || body.trim() == 'null') return null;
    return jsonDecode(body);
  }

  Future<Map<String, dynamic>> bootstrap() async {
    final res = await call('Bootstrap');
    return (res as Map<String, dynamic>?) ?? const {};
  }

  Future<List<SessionMeta>> listSessions() async {
    final res = await call('ListSessions');
    if (res is! List) return const [];
    return res
        .whereType<Map<String, dynamic>>()
        .map(SessionMeta.fromJson)
        .toList();
  }

  Future<SessionMeta> newSession({
    String kind = 'chat',
    String mode = 'build',
    String title = '',
  }) async {
    final res = await call('NewSession', [
      {'kind': kind, 'mode': mode, 'title': title},
    ]);
    return SessionMeta.fromJson(res as Map<String, dynamic>);
  }

  Future<SessionView> openSession(String id) async {
    final res = await call('OpenSession', [id]);
    return SessionView.fromJson(res as Map<String, dynamic>);
  }

  Future<void> send(String sessionId, String text, {String skill = ''}) async {
    await call('Send', [sessionId, text, const <dynamic>[], skill]);
  }

  Future<void> stop(String sessionId) async {
    await call('Stop', [sessionId]);
  }

  Future<void> approve(String sessionId, String reqId, String decision) async {
    await call('Approve', [sessionId, reqId, decision]);
  }

  Future<void> answer(String sessionId, String reqId, String answer) async {
    await call('Answer', [sessionId, reqId, answer]);
  }

  Future<void> setSessionMode(String sessionId, String mode) async {
    await call('SetSessionMode', [sessionId, mode]);
  }

  Future<void> renameSession(String sessionId, String title) async {
    await call('RenameSession', [sessionId, title]);
  }

  Future<void> pinSession(String sessionId, bool pinned) async {
    await call('PinSession', [sessionId, pinned]);
  }

  Future<void> deleteSession(String sessionId) async {
    await call('DeleteSession', [sessionId]);
  }

  Future<ModelInfo> getModel() async {
    final res = await call('GetModel');
    return ModelInfo.fromJson((res as Map<String, dynamic>?) ?? const {});
  }

  Future<ModelInfo> setModel(String provider, String model) async {
    final res = await call('SetModel', [provider, model]);
    return ModelInfo.fromJson((res as Map<String, dynamic>?) ?? const {});
  }

  Future<List<String>> listModels(String provider) async {
    final res = await call('ListModels', [provider]);
    if (res is! List) return const [];
    return res.map((e) => e.toString()).toList();
  }

  void dispose() {
    _disposed = true;
    _sseClient?.close(force: true);
    _http.close(force: true);
    _eventController.close();
    _statusController.close();
  }
}
