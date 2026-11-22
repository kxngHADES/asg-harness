import 'dart:async';

import 'package:material_ui/material_ui.dart';

import '../models/asg_models.dart';
import '../services/asg_client.dart';
import '../theme/asg_theme.dart';
import '../widgets/markdown_view.dart';

/// Main mobile workspace mirroring the ASG Desktop experience:
/// - Navigation drawer = Desktop sidebar (New chat, kind filters, grouped sessions)
/// - Home view = New chat composer (kind + mode + model)
/// - Session view = Live synced chat transcript (user, streaming assistant, tools,
///   permission gates, ask_user cards) reflected in real time on Desktop.
class WorkspaceScreen extends StatefulWidget {
  const WorkspaceScreen({
    super.key,
    required this.host,
    required this.onDisconnect,
  });

  final HostInfo host;
  final VoidCallback onDisconnect;

  @override
  State<WorkspaceScreen> createState() => _WorkspaceScreenState();
}

class _LiveState {
  _LiveState({
    required this.items,
    this.running = false,
  });

  List<TranscriptItem> items;
  bool running;
  String status = '';
}

class _WorkspaceScreenState extends State<WorkspaceScreen> {
  final _scaffoldKey = GlobalKey<ScaffoldState>();
  final _composerController = TextEditingController();
  final _searchController = TextEditingController();
  final _scrollController = ScrollController();

  late final AsgClient _client;
  StreamSubscription<AsgEvent>? _evSub;
  StreamSubscription<ConnectionStateStatus>? _statusSub;
  Timer? _refreshDebounce;

  ConnectionStateStatus _connStatus = ConnectionStateStatus.connecting;
  bool _loading = true;
  ModelInfo? _model;
  List<SessionMeta> _sessions = const [];

  String? _activeSessionId;
  SessionView? _activeView;
  final Map<String, _LiveState> _live = {};

  // New chat defaults (when _activeSessionId == null)
  String _newKind = 'chat';
  String _newMode = 'build';

  // Sidebar filter state
  String _kindFilter = 'all';
  String _searchQuery = '';

  @override
  void initState() {
    super.initState();
    _client = AsgClient(baseUrl: widget.host.baseUrl);
    _evSub = _client.events.listen(_onEvent);
    _statusSub = _client.statusStream.listen((s) {
      if (!mounted) return;
      setState(() => _connStatus = s);
      if (s == ConnectionStateStatus.connected) {
        _refreshSessions();
      }
    });
    _client.startEvents();
    _bootstrap();
  }

  Future<void> _bootstrap() async {
    try {
      final boot = await _client.bootstrap();
      if (!mounted) return;
      final rawSessions = (boot['sessions'] as List<dynamic>?) ?? const [];
      setState(() {
        _sessions = rawSessions
            .whereType<Map<String, dynamic>>()
            .map(SessionMeta.fromJson)
            .toList();
        if (boot['model'] is Map<String, dynamic>) {
          _model = ModelInfo.fromJson(boot['model'] as Map<String, dynamic>);
        }
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() => _loading = false);
      _showToast('Failed to load workspace: $e', isError: true);
    }
  }

  Future<void> _refreshSessions() async {
    try {
      final list = await _client.listSessions();
      if (!mounted) return;
      setState(() {
        _sessions = list;
        if (_activeSessionId != null && _activeView != null) {
          final updated = list.where((s) => s.id == _activeSessionId).firstOrNull;
          if (updated != null) {
            _activeView = SessionView(
              meta: updated,
              items: _activeView!.items,
              running: _activeView!.running,
              workdir: _activeView!.workdir,
            );
          }
        }
      });
    } catch (_) {}
  }

  Future<void> _openSession(String id, {bool closeDrawer = true}) async {
    if (closeDrawer && (_scaffoldKey.currentState?.isDrawerOpen ?? false)) {
      Navigator.of(context).pop();
    }
    setState(() {
      _activeSessionId = id;
    });
    try {
      final v = await _client.openSession(id);
      if (!mounted || _activeSessionId != id) return;
      final existing = _live[id];
      setState(() {
        _activeView = v;
        if (existing == null || !existing.running) {
          _live[id] = _LiveState(items: List.of(v.items), running: v.running);
        }
      });
      _scrollToBottom();
    } catch (e) {
      _showToast('Could not open session: $e', isError: true);
    }
  }

  void _startNewChat() {
    if (_scaffoldKey.currentState?.isDrawerOpen ?? false) {
      Navigator.of(context).pop();
    }
    setState(() {
      _activeSessionId = null;
      _activeView = null;
    });
  }

  Future<void> _submitComposer() async {
    final text = _composerController.text.trim();
    if (text.isEmpty) return;
    _composerController.clear();

    var sid = _activeSessionId;
    if (sid == null) {
      try {
        final created = await _client.newSession(
          kind: _newKind,
          mode: _newMode,
        );
        sid = created.id;
        if (!mounted) return;
        setState(() {
          _activeSessionId = sid;
          _activeView = SessionView(
            meta: created,
            items: const [],
            running: true,
            workdir: created.workdir,
          );
          _live[sid!] = _LiveState(items: [], running: true);
        });
      } catch (e) {
        _composerController.text = text;
        _showToast('Could not start chat: $e', isError: true);
        return;
      }
    }

    final live = _live.putIfAbsent(sid, () => _LiveState(items: []));
    setState(() {
      live.items.add(TranscriptItem(type: 'user', text: text));
      live.running = true;
      live.status = '';
    });
    _scrollToBottom();

    try {
      await _client.send(sid, text);
    } catch (e) {
      if (!mounted) return;
      setState(() {
        live.running = false;
        live.items.add(
          TranscriptItem(type: 'notice', text: '$e', error: true),
        );
      });
    }
  }

  Future<void> _stopActive() async {
    final sid = _activeSessionId;
    if (sid == null) return;
    try {
      await _client.stop(sid);
    } catch (_) {}
  }

  void _onEvent(AsgEvent ev) {
    if (!mounted) return;
    switch (ev.name) {
      case 'asg:sessions':
        _refreshDebounce?.cancel();
        _refreshDebounce = Timer(
          const Duration(milliseconds: 140),
          _refreshSessions,
        );
        break;
      case 'asg:model':
        if (ev.data is Map<String, dynamic>) {
          setState(
            () => _model = ModelInfo.fromJson(ev.data as Map<String, dynamic>),
          );
        }
        break;
      case 'asg:turn':
        if (ev.data is Map<String, dynamic>) {
          _onTurnEvent(ev.data as Map<String, dynamic>);
        }
        break;
    }
  }

  void _onTurnEvent(Map<String, dynamic> ev) {
    final sid = ev['session'] as String?;
    final type = ev['type'] as String?;
    if (sid == null || type == null) return;

    final live = _live.putIfAbsent(
      sid,
      () => _LiveState(items: [], running: true),
    );
    final items = live.items;
    final last = items.isNotEmpty ? items.last : null;

    setState(() {
      switch (type) {
        case 'user':
          final text = (ev['text'] as String?) ?? '';
          if (!(last != null && last.type == 'user' && last.text == text)) {
            items.add(TranscriptItem(type: 'user', text: text));
          }
          live.running = true;
          live.status = '';
          break;
        case 'text':
          final chunk = (ev['text'] as String?) ?? '';
          if (last != null && last.type == 'assistant' && last.pending) {
            last.text += chunk;
          } else {
            items.add(
              TranscriptItem(type: 'assistant', text: chunk, pending: true),
            );
          }
          live.running = true;
          live.status = '';
          break;
        case 'tool_start':
          if (last != null && last.pending) last.pending = false;
          items.add(
            TranscriptItem(
              type: 'tool',
              tool: ToolCallItem(
                id: (ev['id'] as String?) ?? '',
                name: (ev['name'] as String?) ?? '',
                summary: (ev['summary'] as String?) ?? '',
                args: (ev['args'] as String?) ?? '',
                done: false,
              ),
            ),
          );
          break;
        case 'tool_end':
          final tid = (ev['id'] as String?) ?? '';
          ToolCallItem? match;
          for (var i = items.length - 1; i >= 0; i--) {
            final t = items[i].tool;
            if (items[i].type == 'tool' && t != null && t.id == tid && !t.done) {
              match = t;
              break;
            }
          }
          if (match != null) {
            match.result = (ev['result'] as String?) ?? '';
            match.error = (ev['error'] as bool?) ?? false;
            match.done = true;
          } else {
            items.add(
              TranscriptItem(
                type: 'tool',
                tool: ToolCallItem(
                  id: tid,
                  name: (ev['name'] as String?) ?? '',
                  summary: '',
                  result: (ev['result'] as String?) ?? '',
                  error: (ev['error'] as bool?) ?? false,
                  done: true,
                ),
              ),
            );
          }
          break;
        case 'notice':
          items.add(
            TranscriptItem(type: 'notice', text: (ev['text'] as String?) ?? ''),
          );
          break;
        case 'status':
          live.status = (ev['text'] as String?) ?? '';
          break;
        case 'review':
          final pass = ev['pass'];
          final total = ev['total'];
          final lens = ((ev['lens'] as String?) ?? '').split(' — ').first;
          live.status = 'Reviewing $pass/$total · $lens';
          break;
        case 'approval':
          items.add(
            TranscriptItem(
              type: 'approval',
              reqId: (ev['id'] as String?) ?? '',
              toolName: (ev['name'] as String?) ?? '',
              summary: (ev['summary'] as String?) ?? '',
              scope: (ev['scope'] as String?) ?? '',
              args: (ev['args'] as String?) ?? '',
              waiting: true,
            ),
          );
          break;
        case 'approval_done':
          final rid = (ev['id'] as String?) ?? '';
          for (final it in items) {
            if (it.type == 'approval' && it.reqId == rid) {
              it.waiting = false;
            }
          }
          break;
        case 'ask':
          items.add(
            TranscriptItem(
              type: 'ask',
              reqId: (ev['id'] as String?) ?? '',
              question: (ev['question'] as String?) ?? '',
              choices: (ev['choices'] as List<dynamic>?)
                      ?.map((e) => e.toString())
                      .toList() ??
                  const [],
              waiting: true,
            ),
          );
          break;
        case 'ask_done':
          final rid = (ev['id'] as String?) ?? '';
          for (final it in items) {
            if (it.type == 'ask' && it.reqId == rid) {
              it.waiting = false;
              if (ev['answer'] != null) {
                it.answer = ev['answer'].toString();
              }
            }
          }
          break;
        case 'error':
          live.running = false;
          if (last != null && last.pending) last.pending = false;
          items.add(
            TranscriptItem(
              type: 'notice',
              text: (ev['text'] as String?) ?? 'Error',
              error: true,
            ),
          );
          break;
        case 'done':
          live.running = false;
          live.status = '';
          if (last != null && last.pending) last.pending = false;
          if (_activeSessionId == sid) {
            _reloadQuiet(sid);
          }
          break;
      }
    });

    if (_activeSessionId == sid) {
      _scrollToBottom();
    }
  }

  Future<void> _reloadQuiet(String sid) async {
    try {
      final v = await _client.openSession(sid);
      if (!mounted || _activeSessionId != sid) return;
      setState(() {
        _activeView = v;
        _live[sid] = _LiveState(items: List.of(v.items), running: v.running);
      });
    } catch (_) {}
  }

  void _scrollToBottom() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!_scrollController.hasClients) return;
      _scrollController.animateTo(
        _scrollController.position.maxScrollExtent + 120,
        duration: const Duration(milliseconds: 220),
        curve: Curves.easeOut,
      );
    });
  }

  void _showToast(String msg, {bool isError = false}) {
    if (!mounted) return;
    final c = context.asg;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(msg),
        backgroundColor: isError ? c.danger : c.raised,
        behavior: SnackBarBehavior.floating,
      ),
    );
  }

  Future<void> _changeMode(String nextMode) async {
    final sid = _activeSessionId;
    if (sid == null) {
      setState(() => _newMode = nextMode);
      return;
    }
    try {
      await _client.setSessionMode(sid, nextMode);
      await _refreshSessions();
    } catch (e) {
      _showToast('Could not change mode: $e', isError: true);
    }
  }

  Future<void> _showSessionMenu(SessionMeta s) async {
    final c = context.asg;
    await showModalBottomSheet<void>(
      context: context,
      backgroundColor: c.surface,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(18)),
      ),
      builder: (ctx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              leading: Icon(
                s.pinned ? Icons.push_pin_outlined : Icons.push_pin_rounded,
                color: c.text2,
              ),
              title: Text(s.pinned ? 'Unpin' : 'Pin',
                  style: TextStyle(color: c.text)),
              onTap: () async {
                Navigator.pop(ctx);
                await _client.pinSession(s.id, !s.pinned);
                _refreshSessions();
              },
            ),
            ListTile(
              leading: Icon(Icons.edit_rounded, color: c.text2),
              title: Text('Rename', style: TextStyle(color: c.text)),
              onTap: () {
                Navigator.pop(ctx);
                _promptRename(s);
              },
            ),
            ListTile(
              leading: Icon(Icons.delete_outline_rounded, color: c.danger),
              title: Text('Delete', style: TextStyle(color: c.danger)),
              onTap: () async {
                Navigator.pop(ctx);
                try {
                  await _client.deleteSession(s.id);
                  if (_activeSessionId == s.id) _startNewChat();
                  _refreshSessions();
                } catch (e) {
                  _showToast('$e', isError: true);
                }
              },
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _promptRename(SessionMeta s) async {
    final ctrl = TextEditingController(text: s.title);
    final res = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Rename chat'),
        content: TextField(
          controller: ctrl,
          autofocus: true,
          decoration: const InputDecoration(hintText: 'Title'),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, ctrl.text.trim()),
            child: const Text('Save'),
          ),
        ],
      ),
    );
    if (res != null && res.isNotEmpty) {
      await _client.renameSession(s.id, res);
      _refreshSessions();
    }
  }

  @override
  void dispose() {
    _refreshDebounce?.cancel();
    _evSub?.cancel();
    _statusSub?.cancel();
    _client.dispose();
    _composerController.dispose();
    _searchController.dispose();
    _scrollController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final c = context.asg;
    final activeLive = _activeSessionId != null ? _live[_activeSessionId!] : null;
    final isRunning = activeLive?.running ?? false;
    final currentMode = _activeView?.meta.mode ?? _newMode;

    return Scaffold(
      key: _scaffoldKey,
      backgroundColor: c.bg,
      drawer: _buildDrawer(c),
      appBar: AppBar(
        leading: IconButton(
          icon: const Icon(Icons.menu_rounded),
          tooltip: 'Chats',
          onPressed: () => _scaffoldKey.currentState?.openDrawer(),
        ),
        titleSpacing: 4,
        title: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              _activeView?.meta.displayTitle ?? 'New chat',
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(
                fontSize: 16,
                fontWeight: FontWeight.w600,
                color: c.text,
              ),
            ),
            Row(
              children: [
                Container(
                  width: 7,
                  height: 7,
                  decoration: BoxDecoration(
                    shape: BoxShape.circle,
                    color: _connStatus == ConnectionStateStatus.connected
                        ? c.ok
                        : c.warn,
                  ),
                ),
                const SizedBox(width: 5),
                Flexible(
                  child: Text(
                    '${widget.host.hostname} · ${_model?.label ?? widget.host.modelLabel}',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      fontSize: 11.5,
                      color: c.muted,
                      fontWeight: FontWeight.w400,
                    ),
                  ),
                ),
              ],
            ),
          ],
        ),
        actions: [
          PopupMenuButton<String>(
            tooltip: 'Mode',
            onSelected: _changeMode,
            itemBuilder: (_) => const [
              PopupMenuItem(value: 'build', child: Text('Build · makes changes')),
              PopupMenuItem(value: 'plan', child: Text('Plan · read-only')),
              PopupMenuItem(value: 'ask', child: Text('Ask · Q&A')),
            ],
            child: Container(
              margin: const EdgeInsets.symmetric(vertical: 10),
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
              decoration: BoxDecoration(
                color: c.raised,
                borderRadius: BorderRadius.circular(999),
                border: Border.all(color: c.line),
              ),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(
                    currentMode,
                    style: TextStyle(
                      fontFamily: 'monospace',
                      fontSize: 11.5,
                      fontWeight: FontWeight.w600,
                      color: c.brand,
                    ),
                  ),
                  const SizedBox(width: 2),
                  Icon(Icons.expand_more_rounded, size: 15, color: c.muted),
                ],
              ),
            ),
          ),
          IconButton(
            icon: const Icon(Icons.add_comment_outlined),
            tooltip: 'New chat',
            onPressed: _startNewChat,
          ),
        ],
      ),
      body: Column(
        children: [
          if (_connStatus == ConnectionStateStatus.reconnecting)
            Container(
              width: double.infinity,
              color: c.warn.withValues(alpha: 0.16),
              padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 6),
              child: Text(
                'Reconnecting to ${widget.host.hostname}…',
                textAlign: TextAlign.center,
                style: TextStyle(fontSize: 12, color: c.warn),
              ),
            ),
          Expanded(
            child: _loading
                ? Center(child: CircularProgressIndicator(color: c.brand))
                : (_activeSessionId == null
                    ? _buildHomeBody(c)
                    : _buildTranscript(c, activeLive)),
          ),
          if ((activeLive?.status ?? '').isNotEmpty)
            Container(
              width: double.infinity,
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
              color: c.surface,
              child: Row(
                children: [
                  SizedBox(
                    width: 12,
                    height: 12,
                    child: CircularProgressIndicator(
                      strokeWidth: 2,
                      color: c.brand,
                    ),
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      activeLive!.status,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        fontFamily: 'monospace',
                        fontSize: 11.5,
                        color: c.text2,
                      ),
                    ),
                  ),
                ],
              ),
            ),
          _buildComposer(c, isRunning: isRunning),
        ],
      ),
    );
  }

  Widget _buildDrawer(AsgColors c) {
    final filtered = _sessions.where((s) {
      if (_kindFilter != 'all' && s.kind != _kindFilter) return false;
      if (_searchQuery.isNotEmpty) {
        final q = _searchQuery.toLowerCase();
        return s.displayTitle.toLowerCase().contains(q) ||
            s.preview.toLowerCase().contains(q);
      }
      return true;
    }).toList();

    final pinned = filtered.where((s) => s.pinned).toList();
    final rest = filtered.where((s) => !s.pinned).toList();

    return Drawer(
      child: SafeArea(
        child: Column(
          children: [
            // Brand + Connected computer
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 12, 10, 8),
              child: Row(
                children: [
                  ClipRRect(
                    borderRadius: BorderRadius.circular(8),
                    child: Image.asset(
                      'assets/icon/appicon.png',
                      width: 28,
                      height: 28,
                      filterQuality: FilterQuality.none,
                    ),
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          'ASG',
                          style: TextStyle(
                            fontSize: 15,
                            fontWeight: FontWeight.w700,
                            color: c.text,
                          ),
                        ),
                        Text(
                          '${widget.host.hostname} (${widget.host.displayAddress})',
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(
                            fontFamily: 'monospace',
                            fontSize: 10.5,
                            color: c.muted,
                          ),
                        ),
                      ],
                    ),
                  ),
                  IconButton(
                    icon: Icon(Icons.link_off_rounded, size: 20, color: c.muted),
                    tooltip: 'Switch computer',
                    onPressed: () {
                      Navigator.of(context).pop();
                      widget.onDisconnect();
                    },
                  ),
                ],
              ),
            ),

            // New chat button (mirrors Desktop's .new-btn)
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
              child: InkWell(
                onTap: _startNewChat,
                borderRadius: BorderRadius.circular(12),
                child: Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 12,
                    vertical: 10,
                  ),
                  decoration: BoxDecoration(
                    color: c.raised,
                    borderRadius: BorderRadius.circular(12),
                    border: Border.all(color: c.line),
                  ),
                  child: Row(
                    children: [
                      Container(
                        width: 24,
                        height: 24,
                        decoration: BoxDecoration(
                          color: c.brand,
                          shape: BoxShape.circle,
                        ),
                        child: Icon(
                          Icons.add_rounded,
                          size: 16,
                          color: c.brandInk,
                        ),
                      ),
                      const SizedBox(width: 10),
                      Text(
                        'New chat',
                        style: TextStyle(
                          fontWeight: FontWeight.w600,
                          color: c.text,
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            ),

            // Search input
            Padding(
              padding: const EdgeInsets.fromLTRB(12, 4, 12, 8),
              child: TextField(
                controller: _searchController,
                onChanged: (v) => setState(() => _searchQuery = v.trim()),
                style: TextStyle(fontSize: 13.5, color: c.text),
                decoration: InputDecoration(
                  hintText: 'Search chats…',
                  hintStyle: TextStyle(fontSize: 13.5, color: c.muted),
                  prefixIcon: Icon(Icons.search_rounded, size: 18, color: c.muted),
                  isDense: true,
                  filled: true,
                  fillColor: c.surface,
                  contentPadding: const EdgeInsets.symmetric(vertical: 10),
                  border: OutlineInputBorder(
                    borderRadius: BorderRadius.circular(10),
                    borderSide: BorderSide(color: c.line),
                  ),
                  enabledBorder: OutlineInputBorder(
                    borderRadius: BorderRadius.circular(10),
                    borderSide: BorderSide(color: c.line),
                  ),
                ),
              ),
            ),

            // Kind filter chips
            SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              padding: const EdgeInsets.symmetric(horizontal: 12),
              child: Row(
                children: [
                  for (final k in const ['all', 'chat', 'code', 'artifact', 'project'])
                    Padding(
                      padding: const EdgeInsets.only(right: 6),
                      child: ChoiceChip(
                        label: Text(
                          k == 'all' ? 'All' : k[0].toUpperCase() + k.substring(1),
                          style: TextStyle(
                            fontSize: 12,
                            color: _kindFilter == k ? c.brandInk : c.text2,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                        selected: _kindFilter == k,
                        selectedColor: c.brand,
                        backgroundColor: c.surface,
                        side: BorderSide(color: c.line),
                        showCheckmark: false,
                        visualDensity: VisualDensity.compact,
                        onSelected: (_) => setState(() => _kindFilter = k),
                      ),
                    ),
                ],
              ),
            ),
            const SizedBox(height: 6),
            Divider(height: 1, color: c.line),

            // Sessions list
            Expanded(
              child: RefreshIndicator(
                color: c.brand,
                onRefresh: _refreshSessions,
                child: filtered.isEmpty
                    ? ListView(
                        padding: const EdgeInsets.all(24),
                        children: [
                          Text(
                            'No chats yet. Start one above!',
                            textAlign: TextAlign.center,
                            style: TextStyle(color: c.muted, fontSize: 13),
                          ),
                        ],
                      )
                    : ListView(
                        padding: const EdgeInsets.symmetric(
                          horizontal: 8,
                          vertical: 8,
                        ),
                        children: [
                          if (pinned.isNotEmpty) ...[
                            _groupHeader(c, 'Pinned'),
                            ...pinned.map((s) => _sessionTile(c, s)),
                            const SizedBox(height: 10),
                          ],
                          if (rest.isNotEmpty) ...[
                            _groupHeader(c, 'Recent'),
                            ...rest.map((s) => _sessionTile(c, s)),
                          ],
                        ],
                      ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _groupHeader(AsgColors c, String label) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(10, 6, 10, 4),
      child: Text(
        label.toUpperCase(),
        style: TextStyle(
          fontFamily: 'monospace',
          fontSize: 10.5,
          fontWeight: FontWeight.w600,
          letterSpacing: 0.8,
          color: c.muted,
        ),
      ),
    );
  }

  Widget _sessionTile(AsgColors c, SessionMeta s) {
    final selected = s.id == _activeSessionId;
    final running = _live[s.id]?.running ?? false;
    final kindColor = c.colorForKind(s.kind);

    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 1.5),
      child: Material(
        color: selected ? c.raised : Colors.transparent,
        borderRadius: BorderRadius.circular(10),
        child: InkWell(
          onTap: () => _openSession(s.id),
          onLongPress: () => _showSessionMenu(s),
          borderRadius: BorderRadius.circular(10),
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 9),
            child: Row(
              children: [
                Container(
                  width: 26,
                  height: 26,
                  decoration: BoxDecoration(
                    color: kindColor.withValues(alpha: 0.14),
                    borderRadius: BorderRadius.circular(7),
                  ),
                  child: Icon(
                    AsgColors.iconForKind(s.kind),
                    size: 15,
                    color: kindColor,
                  ),
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        s.displayTitle,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(
                          fontSize: 13.5,
                          fontWeight:
                              selected ? FontWeight.w600 : FontWeight.w500,
                          color: c.text,
                        ),
                      ),
                      if (s.preview.isNotEmpty)
                        Text(
                          s.preview,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(fontSize: 11.5, color: c.muted),
                        ),
                    ],
                  ),
                ),
                if (running)
                  Container(
                    width: 8,
                    height: 8,
                    margin: const EdgeInsets.only(left: 6),
                    decoration: BoxDecoration(
                      color: c.brand,
                      shape: BoxShape.circle,
                    ),
                  )
                else
                  IconButton(
                    icon: Icon(Icons.more_horiz_rounded, size: 18, color: c.muted),
                    visualDensity: VisualDensity.compact,
                    onPressed: () => _showSessionMenu(s),
                  ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildHomeBody(AsgColors c) {
    return Center(
      child: SingleChildScrollView(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ClipRRect(
              borderRadius: BorderRadius.circular(20),
              child: Image.asset(
                'assets/icon/appicon.png',
                width: 72,
                height: 72,
                filterQuality: FilterQuality.none,
              ),
            ),
            const SizedBox(height: 16),
            Text(
              'What are we working on?',
              textAlign: TextAlign.center,
              style: TextStyle(
                fontSize: 22,
                fontWeight: FontWeight.w700,
                color: c.text,
                letterSpacing: -0.3,
              ),
            ),
            const SizedBox(height: 6),
            Text(
              'Connected to ${widget.host.hostname} · synced live with ASG Desktop',
              textAlign: TextAlign.center,
              style: TextStyle(fontSize: 13, color: c.muted),
            ),
            const SizedBox(height: 20),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              alignment: WrapAlignment.center,
              children: [
                for (final entry in const [
                  ('chat', 'Chat'),
                  ('artifact', 'Artifact'),
                ])
                  ChoiceChip(
                    avatar: Icon(
                      AsgColors.iconForKind(entry.$1),
                      size: 16,
                      color: _newKind == entry.$1
                          ? c.brandInk
                          : c.colorForKind(entry.$1),
                    ),
                    label: Text(
                      entry.$2,
                      style: TextStyle(
                        color: _newKind == entry.$1 ? c.brandInk : c.text,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                    selected: _newKind == entry.$1,
                    selectedColor: c.brand,
                    backgroundColor: c.surface,
                    side: BorderSide(color: c.line),
                    showCheckmark: false,
                    onSelected: (_) => setState(() => _newKind = entry.$1),
                  ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildTranscript(AsgColors c, _LiveState? live) {
    final items = live?.items ?? const <TranscriptItem>[];
    if (items.isEmpty) {
      return Center(
        child: Text(
          'Send a message to start this chat.',
          style: TextStyle(color: c.muted),
        ),
      );
    }
    return ListView.separated(
      controller: _scrollController,
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 24),
      itemCount: items.length,
      separatorBuilder: (_, _) => const SizedBox(height: 14),
      itemBuilder: (ctx, i) => _buildItem(c, items[i]),
    );
  }

  Widget _buildItem(AsgColors c, TranscriptItem it) {
    switch (it.type) {
      case 'user':
        return Align(
          alignment: Alignment.centerRight,
          child: Container(
            constraints: BoxConstraints(
              maxWidth: MediaQuery.of(context).size.width * 0.82,
            ),
            padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
            decoration: BoxDecoration(
              color: c.userBubble,
              borderRadius: const BorderRadius.only(
                topLeft: Radius.circular(16),
                topRight: Radius.circular(16),
                bottomLeft: Radius.circular(16),
                bottomRight: Radius.circular(4),
              ),
              border: Border.all(color: c.line),
            ),
            child: SelectableText(
              it.text,
              style: TextStyle(fontSize: 15, height: 1.45, color: c.text),
            ),
          ),
        );

      case 'assistant':
        return Padding(
          padding: const EdgeInsets.only(right: 12),
          child: SimpleMarkdown(text: it.text),
        );

      case 'tool':
        return _ToolCard(tool: it.tool!);

      case 'approval':
        return _ApprovalCard(
          item: it,
          onDecide: (decision) async {
            final sid = _activeSessionId;
            if (sid == null) return;
            try {
              await _client.approve(sid, it.reqId, decision);
            } catch (e) {
              _showToast('$e', isError: true);
            }
          },
        );

      case 'ask':
        return _AskCard(
          item: it,
          onAnswer: (answer) async {
            final sid = _activeSessionId;
            if (sid == null) return;
            try {
              await _client.answer(sid, it.reqId, answer);
            } catch (e) {
              _showToast('$e', isError: true);
            }
          },
        );

      default:
        return Container(
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
          decoration: BoxDecoration(
            color: it.error
                ? c.danger.withValues(alpha: 0.12)
                : c.surface,
            borderRadius: BorderRadius.circular(10),
            border: Border.all(
              color: it.error ? c.danger.withValues(alpha: 0.35) : c.line,
            ),
          ),
          child: Text(
            it.text,
            style: TextStyle(
              fontSize: 13,
              color: it.error ? c.danger : c.text2,
            ),
          ),
        );
    }
  }

  Widget _buildComposer(AsgColors c, {required bool isRunning}) {
    return Container(
      decoration: BoxDecoration(
        color: c.bg,
        border: Border(top: BorderSide(color: c.line)),
      ),
      child: SafeArea(
        top: false,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(12, 10, 12, 10),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.end,
            children: [
              Expanded(
                child: TextField(
                  controller: _composerController,
                  minLines: 1,
                  maxLines: 5,
                  textInputAction: TextInputAction.send,
                  onSubmitted: (_) {
                    if (!isRunning) _submitComposer();
                  },
                  style: TextStyle(fontSize: 15, color: c.text),
                  decoration: InputDecoration(
                    hintText: 'Message ASG…',
                    hintStyle: TextStyle(color: c.muted),
                    filled: true,
                    fillColor: c.surface,
                    contentPadding: const EdgeInsets.symmetric(
                      horizontal: 14,
                      vertical: 12,
                    ),
                    border: OutlineInputBorder(
                      borderRadius: BorderRadius.circular(20),
                      borderSide: BorderSide(color: c.line),
                    ),
                    enabledBorder: OutlineInputBorder(
                      borderRadius: BorderRadius.circular(20),
                      borderSide: BorderSide(color: c.line),
                    ),
                    focusedBorder: OutlineInputBorder(
                      borderRadius: BorderRadius.circular(20),
                      borderSide: BorderSide(color: c.brand, width: 1.4),
                    ),
                  ),
                ),
              ),
              const SizedBox(width: 8),
              SizedBox(
                width: 44,
                height: 44,
                child: FilledButton(
                  onPressed: isRunning ? _stopActive : _submitComposer,
                  style: FilledButton.styleFrom(
                    padding: EdgeInsets.zero,
                    backgroundColor: isRunning ? c.danger : c.brand,
                    foregroundColor: isRunning ? Colors.white : c.brandInk,
                    shape: const CircleBorder(),
                  ),
                  child: Icon(
                    isRunning ? Icons.stop_rounded : Icons.arrow_upward_rounded,
                    size: 22,
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _ToolCard extends StatefulWidget {
  const _ToolCard({required this.tool});

  final ToolCallItem tool;

  @override
  State<_ToolCard> createState() => _ToolCardState();
}

class _ToolCardState extends State<_ToolCard> {
  bool _open = false;

  @override
  Widget build(BuildContext context) {
    final c = context.asg;
    final t = widget.tool;
    return Container(
      decoration: BoxDecoration(
        color: c.surface,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: c.line),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          InkWell(
            onTap: () => setState(() => _open = !_open),
            borderRadius: BorderRadius.circular(12),
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
              child: Row(
                children: [
                  if (!t.done)
                    SizedBox(
                      width: 14,
                      height: 14,
                      child: CircularProgressIndicator(
                        strokeWidth: 2,
                        color: c.brand,
                      ),
                    )
                  else
                    Icon(
                      t.error
                          ? Icons.error_outline_rounded
                          : Icons.check_circle_outline_rounded,
                      size: 16,
                      color: t.error ? c.danger : c.ok,
                    ),
                  const SizedBox(width: 8),
                  Text(
                    t.name,
                    style: TextStyle(
                      fontFamily: 'monospace',
                      fontSize: 12.5,
                      fontWeight: FontWeight.w600,
                      color: c.text,
                    ),
                  ),
                  if (t.summary.isNotEmpty) ...[
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        t.summary,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(fontSize: 12, color: c.muted),
                      ),
                    ),
                  ] else
                    const Spacer(),
                  Icon(
                    _open
                        ? Icons.expand_less_rounded
                        : Icons.expand_more_rounded,
                    size: 18,
                    color: c.muted,
                  ),
                ],
              ),
            ),
          ),
          if (_open && t.result.isNotEmpty)
            Container(
              padding: const EdgeInsets.all(10),
              decoration: BoxDecoration(
                color: c.codeBg,
                borderRadius: const BorderRadius.vertical(
                  bottom: Radius.circular(11),
                ),
                border: Border(top: BorderSide(color: c.line)),
              ),
              child: SelectableText(
                t.result,
                style: const TextStyle(
                  fontFamily: 'monospace',
                  fontSize: 11.5,
                  color: Color(0xFFECEBE5),
                  height: 1.45,
                ),
              ),
            ),
        ],
      ),
    );
  }
}

class _ApprovalCard extends StatelessWidget {
  const _ApprovalCard({required this.item, required this.onDecide});

  final TranscriptItem item;
  final void Function(String decision) onDecide;

  @override
  Widget build(BuildContext context) {
    final c = context.asg;
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: c.warn.withValues(alpha: 0.1),
        borderRadius: BorderRadius.circular(14),
        border: Border.all(color: c.warn.withValues(alpha: 0.45)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(Icons.shield_outlined, size: 18, color: c.warn),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  'Permission needed · ${item.toolName}',
                  style: TextStyle(
                    fontWeight: FontWeight.w700,
                    fontSize: 13.5,
                    color: c.text,
                  ),
                ),
              ),
            ],
          ),
          if (item.summary.isNotEmpty) ...[
            const SizedBox(height: 6),
            Text(
              item.summary,
              style: TextStyle(
                fontFamily: 'monospace',
                fontSize: 12.5,
                color: c.text2,
              ),
            ),
          ],
          const SizedBox(height: 10),
          if (!item.waiting)
            Text(
              'Answered',
              style: TextStyle(fontSize: 12, color: c.muted),
            )
          else
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                FilledButton(
                  onPressed: () => onDecide('allow'),
                  style: FilledButton.styleFrom(
                    backgroundColor: c.brand,
                    foregroundColor: c.brandInk,
                  ),
                  child: const Text('Allow'),
                ),
                OutlinedButton(
                  onPressed: () => onDecide('always'),
                  child: const Text('Always here'),
                ),
                TextButton(
                  onPressed: () => onDecide('deny'),
                  style: TextButton.styleFrom(foregroundColor: c.danger),
                  child: const Text('Deny'),
                ),
              ],
            ),
        ],
      ),
    );
  }
}

class _AskCard extends StatefulWidget {
  const _AskCard({required this.item, required this.onAnswer});

  final TranscriptItem item;
  final void Function(String answer) onAnswer;

  @override
  State<_AskCard> createState() => _AskCardState();
}

class _AskCardState extends State<_AskCard> {
  final _ctrl = TextEditingController();

  @override
  void dispose() {
    _ctrl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final c = context.asg;
    final it = widget.item;
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: c.surface,
        borderRadius: BorderRadius.circular(14),
        border: Border.all(color: c.kindArtifact.withValues(alpha: 0.5)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            it.question,
            style: TextStyle(
              fontWeight: FontWeight.w600,
              fontSize: 14,
              color: c.text,
            ),
          ),
          const SizedBox(height: 10),
          if (!it.waiting)
            Text(
              it.answer.isNotEmpty ? 'Answered: ${it.answer}' : 'Answered',
              style: TextStyle(fontSize: 12.5, color: c.muted),
            )
          else ...[
            if (it.choices.isNotEmpty)
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: it.choices
                    .map(
                      (choice) => ActionChip(
                        label: Text(choice),
                        onPressed: () => widget.onAnswer(choice),
                      ),
                    )
                    .toList(),
              ),
            const SizedBox(height: 8),
            Row(
              children: [
                Expanded(
                  child: TextField(
                    controller: _ctrl,
                    decoration: const InputDecoration(
                      hintText: 'Or type an answer…',
                      isDense: true,
                    ),
                    onSubmitted: (v) {
                      if (v.trim().isNotEmpty) widget.onAnswer(v.trim());
                    },
                  ),
                ),
                const SizedBox(width: 8),
                FilledButton(
                  onPressed: () {
                    final v = _ctrl.text.trim();
                    if (v.isNotEmpty) widget.onAnswer(v);
                  },
                  style: FilledButton.styleFrom(
                    backgroundColor: c.brand,
                    foregroundColor: c.brandInk,
                  ),
                  child: const Text('Reply'),
                ),
              ],
            ),
          ],
        ],
      ),
    );
  }
}
