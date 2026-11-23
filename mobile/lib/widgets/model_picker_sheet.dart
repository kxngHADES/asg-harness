import 'package:material_ui/material_ui.dart';

import '../models/asg_models.dart';
import '../services/asg_client.dart';
import '../theme/asg_theme.dart';

/// Bottom sheet for switching the active provider and model on the connected
/// ASG Desktop instance. Changes reflect immediately on both mobile & desktop.
class ModelPickerSheet extends StatefulWidget {
  const ModelPickerSheet({
    super.key,
    required this.client,
    required this.current,
    required this.onChanged,
  });

  final AsgClient client;
  final ModelInfo? current;
  final void Function(ModelInfo updated) onChanged;

  @override
  State<ModelPickerSheet> createState() => _ModelPickerSheetState();
}

class _ModelPickerSheetState extends State<ModelPickerSheet> {
  final _searchCtrl = TextEditingController();

  late String _provider;
  late List<String> _providers;
  List<String> _models = const [];
  bool _loading = false;
  bool _saving = false;
  String? _error;
  String _query = '';

  @override
  void initState() {
    super.initState();
    _providers = widget.current?.providers ?? const [];
    _provider = widget.current?.provider ??
        (_providers.isNotEmpty ? _providers.first : '');
    if (_provider.isNotEmpty) {
      _loadModels(_provider);
    } else {
      _refreshCurrentModel();
    }
  }

  Future<void> _refreshCurrentModel() async {
    setState(() => _loading = true);
    try {
      final info = await widget.client.getModel();
      if (!mounted) return;
      setState(() {
        _providers = info.providers;
        _provider = info.provider.isNotEmpty
            ? info.provider
            : (_providers.isNotEmpty ? _providers.first : '');
      });
      if (_provider.isNotEmpty) {
        await _loadModels(_provider);
      } else {
        setState(() => _loading = false);
      }
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = '$e';
      });
    }
  }

  Future<void> _loadModels(String provider) async {
    setState(() {
      _provider = provider;
      _loading = true;
      _error = null;
      _models = const [];
    });
    try {
      final list = await widget.client.listModels(provider);
      if (!mounted || _provider != provider) return;
      setState(() {
        _models = list;
        _loading = false;
      });
    } catch (e) {
      if (!mounted || _provider != provider) return;
      setState(() {
        _loading = false;
        _error = e.toString().replaceFirst('HttpException: ', '');
      });
    }
  }

  Future<void> _pickModel(String model) async {
    if (_saving || _provider.isEmpty) return;
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      final updated = await widget.client.setModel(_provider, model);
      if (!mounted) return;
      widget.onChanged(updated);
      Navigator.of(context).pop();
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _saving = false;
        _error = e.toString().replaceFirst('HttpException: ', '');
      });
    }
  }

  @override
  void dispose() {
    _searchCtrl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final c = context.asg;
    final currentModel = widget.current?.model ?? '';
    final currentProvider = widget.current?.provider ?? '';
    final filtered = _models.where((m) {
      if (_query.isEmpty) return true;
      return m.toLowerCase().contains(_query.toLowerCase());
    }).toList();

    return DraggableScrollableSheet(
      initialChildSize: 0.72,
      minChildSize: 0.45,
      maxChildSize: 0.92,
      expand: false,
      builder: (ctx, scrollCtrl) => Container(
        decoration: BoxDecoration(
          color: c.surface,
          borderRadius: const BorderRadius.vertical(top: Radius.circular(20)),
          border: Border(top: BorderSide(color: c.line)),
        ),
        child: Column(
          children: [
            // Drag handle
            Container(
              width: 36,
              height: 4,
              margin: const EdgeInsets.only(top: 10, bottom: 10),
              decoration: BoxDecoration(
                color: c.line,
                borderRadius: BorderRadius.circular(99),
              ),
            ),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 18),
              child: Row(
                children: [
                  Text(
                    'Switch model',
                    style: TextStyle(
                      fontSize: 17,
                      fontWeight: FontWeight.w700,
                      color: c.text,
                    ),
                  ),
                  const Spacer(),
                  if (_saving)
                    SizedBox(
                      width: 18,
                      height: 18,
                      child: CircularProgressIndicator(
                        strokeWidth: 2,
                        color: c.brand,
                      ),
                    )
                  else
                    IconButton(
                      icon: Icon(Icons.refresh_rounded, color: c.muted),
                      tooltip: 'Refresh models',
                      onPressed: _provider.isEmpty
                          ? null
                          : () => _loadModels(_provider),
                    ),
                ],
              ),
            ),

            // Provider chips
            if (_providers.isNotEmpty)
              SizedBox(
                height: 44,
                child: ListView.separated(
                  scrollDirection: Axis.horizontal,
                  padding: const EdgeInsets.symmetric(horizontal: 18),
                  itemCount: _providers.length,
                  separatorBuilder: (_, _) => const SizedBox(width: 8),
                  itemBuilder: (_, i) {
                    final p = _providers[i];
                    final selected = p == _provider;
                    return ChoiceChip(
                      label: Text(
                        p,
                        style: TextStyle(
                          fontFamily: 'monospace',
                          fontSize: 12.5,
                          fontWeight: FontWeight.w600,
                          color: selected ? c.brandInk : c.text2,
                        ),
                      ),
                      selected: selected,
                      selectedColor: c.brand,
                      backgroundColor: c.raised,
                      side: BorderSide(color: c.line),
                      showCheckmark: false,
                      onSelected: (_) => _loadModels(p),
                    );
                  },
                ),
              ),

            // Filter / custom model input
            Padding(
              padding: const EdgeInsets.fromLTRB(18, 10, 18, 8),
              child: TextField(
                controller: _searchCtrl,
                onChanged: (v) => setState(() => _query = v.trim()),
                onSubmitted: (v) {
                  if (v.trim().isNotEmpty) _pickModel(v.trim());
                },
                style: TextStyle(
                  fontFamily: 'monospace',
                  fontSize: 13.5,
                  color: c.text,
                ),
                decoration: InputDecoration(
                  hintText: 'Filter models or type a model name…',
                  hintStyle: TextStyle(fontSize: 13, color: c.muted),
                  prefixIcon: Icon(Icons.search_rounded, size: 18, color: c.muted),
                  isDense: true,
                  filled: true,
                  fillColor: c.raised,
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
                    borderSide: BorderSide(color: c.brand, width: 1.4),
                  ),
                ),
              ),
            ),

            if (_error != null)
              Padding(
                padding: const EdgeInsets.fromLTRB(18, 0, 18, 8),
                child: Container(
                  width: double.infinity,
                  padding: const EdgeInsets.all(10),
                  decoration: BoxDecoration(
                    color: c.danger.withValues(alpha: 0.12),
                    borderRadius: BorderRadius.circular(10),
                  ),
                  child: Text(
                    _error!,
                    style: TextStyle(fontSize: 12, color: c.danger),
                  ),
                ),
              ),

            Expanded(
              child: _loading
                  ? Center(child: CircularProgressIndicator(color: c.brand))
                  : ListView(
                      controller: scrollCtrl,
                      padding: const EdgeInsets.fromLTRB(12, 4, 12, 24),
                      children: [
                        if (_query.isNotEmpty &&
                            !filtered.contains(_query) &&
                            _provider.isNotEmpty)
                          ListTile(
                            shape: RoundedRectangleBorder(
                              borderRadius: BorderRadius.circular(12),
                            ),
                            leading: Icon(Icons.add_circle_outline, color: c.brand),
                            title: Text(
                              'Use "$_query"',
                              style: TextStyle(
                                fontFamily: 'monospace',
                                fontSize: 13.5,
                                color: c.text,
                              ),
                            ),
                            subtitle: Text(
                              'Set custom model on $_provider',
                              style: TextStyle(fontSize: 12, color: c.muted),
                            ),
                            onTap: () => _pickModel(_query),
                          ),
                        if (filtered.isEmpty && _query.isEmpty)
                          Padding(
                            padding: const EdgeInsets.all(24),
                            child: Text(
                              'No models returned by $_provider. Type a model name above to set it.',
                              textAlign: TextAlign.center,
                              style: TextStyle(color: c.muted, fontSize: 13),
                            ),
                          ),
                        for (final m in filtered)
                          ListTile(
                            shape: RoundedRectangleBorder(
                              borderRadius: BorderRadius.circular(12),
                            ),
                            tileColor:
                                (m == currentModel && _provider == currentProvider)
                                    ? c.raised
                                    : null,
                            title: Text(
                              m,
                              style: TextStyle(
                                fontFamily: 'monospace',
                                fontSize: 13.5,
                                fontWeight: (m == currentModel &&
                                        _provider == currentProvider)
                                    ? FontWeight.w700
                                    : FontWeight.w500,
                                color: c.text,
                              ),
                            ),
                            trailing: (m == currentModel &&
                                    _provider == currentProvider)
                                ? Icon(Icons.check_circle_rounded,
                                    size: 18, color: c.brand)
                                : null,
                            onTap: () => _pickModel(m),
                          ),
                      ],
                    ),
            ),
          ],
        ),
      ),
    );
  }
}
