package com.asg.asg_harness

import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.provider.Settings
import androidx.core.content.FileProvider
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.io.File

class MainActivity : FlutterActivity() {
    companion object {
        private const val CHANNEL = "com.asg.asg_harness/updater"
    }

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, CHANNEL).setMethodCallHandler { call, result ->
            when (call.method) {
                "getAppVersion" -> {
                    try {
                        val pkg = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                            packageManager.getPackageInfo(packageName, PackageManager.PackageInfoFlags.of(0))
                        } else {
                            @Suppress("DEPRECATION")
                            packageManager.getPackageInfo(packageName, 0)
                        }
                        val buildCode = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
                            pkg.longVersionCode
                        } else {
                            @Suppress("DEPRECATION")
                            pkg.versionCode.toLong()
                        }
                        result.success(
                            mapOf(
                                "version" to (pkg.versionName ?: "0.0.0"),
                                "buildNumber" to buildCode.toString(),
                            )
                        )
                    } catch (e: Exception) {
                        result.error("VERSION_ERROR", e.message, null)
                    }
                }

                "getCacheDir" -> {
                    result.success(cacheDir.absolutePath)
                }

                "installApk" -> {
                    val path = call.argument<String>("path")
                    if (path.isNullOrBlank()) {
                        result.error("INVALID_PATH", "APK path is required", null)
                        return@setMethodCallHandler
                    }
                    val file = File(path)
                    if (!file.exists()) {
                        result.error("FILE_NOT_FOUND", "APK file not found: $path", null)
                        return@setMethodCallHandler
                    }
                    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O && !packageManager.canRequestPackageInstalls()) {
                        val permIntent = Intent(
                            Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES,
                            Uri.parse("package:$packageName")
                        ).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                        startActivity(permIntent)
                        result.success("needs_permission")
                        return@setMethodCallHandler
                    }
                    try {
                        val uri = FileProvider.getUriForFile(
                            this,
                            "$packageName.fileprovider",
                            file
                        )
                        val intent = Intent(Intent.ACTION_VIEW).apply {
                            setDataAndType(uri, "application/vnd.android.package-archive")
                            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
                            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                        }
                        startActivity(intent)
                        result.success("started")
                    } catch (e: Exception) {
                        result.error("INSTALL_ERROR", e.message, null)
                    }
                }

                else -> result.notImplemented()
            }
        }
    }
}
