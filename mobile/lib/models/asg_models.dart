class HostInfo {
  const HostInfo({
    required this.baseUrl,
    required this.hostname,
    required this.version,
    required this.provider,
    required this.model,
    required this.addresses,
  });

  final String baseUrl;
  final String hostname;
  final String version;
  final String provider;
  final String model;
  final List<String> addresses;

  String get displayAddress {
    final uri = Uri.tryParse(baseUrl);
    if (uri == null) return baseUrl;
    return uri.hasPort ? '${uri.host}:${uri.port}' : uri.host;
  }

  String get modelLabel {
    if (model.isEmpty && provider.isEmpty) return 'No model configured';
    if (provider.isEmpty) return model;
    if (model.isEmpty) return provider;
    return '$provider · $model';
  }

  factory HostInfo.fromJson(String baseUrl, Map<String, dynamic> j) {
    return HostInfo(
      baseUrl: baseUrl,
      hostname: (j['hostname'] as String?)?.trim().isNotEmpty == true
          ? (j['hostname'] as String).trim()
          : Uri.tryParse(baseUrl)?.host ?? baseUrl,
      version: (j['version'] as String?) ?? '',
      provider: (j['provider'] as String?) ?? '',
      model: (j['model'] as String?) ?? '',
      addresses: (j['addresses'] as List<dynamic>?)
              ?.map((e) => e.toString())
              .toList() ??
          const [],
    );
  }
}

class ModelInfo {
  const ModelInfo({
    required this.provider,
    required this.model,
    required this.type,
    required this.providers,
  });

  final String provider;
  final String model;
  final String type;
  final List<String> providers;

  String get label {
    if (model.isNotEmpty) return model;
    if (provider.isNotEmpty) return provider;
    return 'No model';
  }

  factory ModelInfo.fromJson(Map<String, dynamic> j) {
    return ModelInfo(
      provider: (j['provider'] as String?) ?? '',
      model: (j['model'] as String?) ?? '',
      type: (j['type'] as String?) ?? '',
      providers: (j['providers'] as List<dynamic>?)
              ?.map((e) => e.toString())
              .toList() ??
          const [],
    );
  }
}

class SessionMeta {
  const SessionMeta({
    required this.id,
    required this.title,
    required this.kind,
    required this.mode,
    required this.updated,
    required this.pinned,
    required this.preview,
    required this.model,
    required this.projectId,
    required this.workdir,
    required this.branch,
  });

  final String id;
  final String title;
  final String kind;
  final String mode;
  final DateTime? updated;
  final bool pinned;
  final String preview;
  final String model;
  final String projectId;
  final String workdir;
  final String branch;

  String get displayTitle => title.trim().isEmpty ? 'Untitled' : title.trim();

  factory SessionMeta.fromJson(Map<String, dynamic> j) {
    return SessionMeta(
      id: (j['id'] as String?) ?? '',
      title: (j['title'] as String?) ?? '',
      kind: (j['kind'] as String?) ?? 'chat',
      mode: (j['mode'] as String?) ?? 'build',
      updated: DateTime.tryParse((j['updated'] as String?) ?? ''),
      pinned: (j['pinned'] as bool?) ?? false,
      preview: (j['preview'] as String?) ?? '',
      model: (j['model'] as String?) ?? '',
      projectId: (j['project_id'] as String?) ?? '',
      workdir: (j['workdir'] as String?) ?? '',
      branch: (j['branch'] as String?) ?? '',
    );
  }
}

class ToolCallItem {
  ToolCallItem({
    required this.id,
    required this.name,
    required this.summary,
    this.args = '',
    this.result = '',
    this.error = false,
    this.done = false,
  });

  final String id;
  final String name;
  final String summary;
  final String args;
  String result;
  bool error;
  bool done;

  factory ToolCallItem.fromJson(Map<String, dynamic> j) {
    return ToolCallItem(
      id: (j['id'] as String?) ?? '',
      name: (j['name'] as String?) ?? '',
      summary: (j['summary'] as String?) ?? '',
      args: (j['args'] as String?) ?? '',
      result: (j['result'] as String?) ?? '',
      error: (j['error'] as bool?) ?? false,
      done: (j['done'] as bool?) ?? false,
    );
  }
}

class TranscriptItem {
  TranscriptItem({
    required this.type,
    this.text = '',
    this.tool,
    this.pending = false,
    this.error = false,
    this.reqId = '',
    this.toolName = '',
    this.summary = '',
    this.scope = '',
    this.args = '',
    this.question = '',
    this.choices = const [],
    this.answer = '',
    this.waiting = false,
  });

  /// user | assistant | tool | notice | approval | ask
  final String type;
  String text;
  ToolCallItem? tool;
  bool pending;
  bool error;

  // For approval / ask cards
  final String reqId;
  final String toolName;
  final String summary;
  final String scope;
  final String args;
  final String question;
  final List<String> choices;
  String answer;
  bool waiting;

  factory TranscriptItem.fromJson(Map<String, dynamic> j) {
    return TranscriptItem(
      type: (j['type'] as String?) ?? 'notice',
      text: (j['text'] as String?) ?? '',
      tool: j['tool'] is Map<String, dynamic>
          ? ToolCallItem.fromJson(j['tool'] as Map<String, dynamic>)
          : null,
      pending: (j['pending'] as bool?) ?? false,
    );
  }
}

class SessionView {
  const SessionView({
    required this.meta,
    required this.items,
    required this.running,
    required this.workdir,
  });

  final SessionMeta meta;
  final List<TranscriptItem> items;
  final bool running;
  final String workdir;

  factory SessionView.fromJson(Map<String, dynamic> j) {
    final rawItems = (j['items'] as List<dynamic>?) ?? const [];
    return SessionView(
      meta: SessionMeta.fromJson(
        (j['meta'] as Map<String, dynamic>?) ?? const {},
      ),
      items: rawItems
          .whereType<Map<String, dynamic>>()
          .map(TranscriptItem.fromJson)
          .toList(),
      running: (j['running'] as bool?) ?? false,
      workdir: (j['workdir'] as String?) ?? '',
    );
  }
}

class AsgEvent {
  const AsgEvent({required this.name, required this.data});

  final String name;
  final dynamic data;
}
