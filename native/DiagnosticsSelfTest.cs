using System;
using System.IO;
using System.Text;
using System.Threading.Tasks;

namespace Cmnet.Native;

/// <summary>
/// Gate 3.4 local redaction + breadcrumb harness (no network, no paid crash cloud).
/// Invoked via <c>Cmnet.Native.exe --self-test-diagnostics [evidenceDir]</c>.
/// </summary>
internal static class DiagnosticsSelfTest
{
    public static Task<int> RunAsync(string? evidenceDir)
    {
        var dir = ResolveEvidence(evidenceDir);
        Directory.CreateDirectory(dir);
        var log = new StringBuilder();
        var failed = 0;

        void Pass(string name) => log.AppendLine("PASS  " + name);
        void Fail(string name, string why)
        {
            failed++;
            log.AppendLine("FAIL  " + name + " — " + why);
        }

        try
        {
            // --- Redaction unit cases ---
            const string ipc = "A1B2C3D4E5F60718293A4B5C6D7E8F90AABBCCDDEEFF00112233445566778899";
            // Assemble JWT-shaped fixture at runtime so the full token never
            // appears as a source literal (GitGuardian false-positive on tests).
            static string B64Url(string s) =>
                Convert.ToBase64String(Encoding.UTF8.GetBytes(s))
                    .TrimEnd('=').Replace('+', '-').Replace('/', '_');
            var jwt = string.Join('.',
                B64Url("""{"alg":"HS256","typ":"JWT"}"""),
                B64Url("""{"sub":"1234567890"}"""),
                "signaturepartxx");
            const string account = "76561198012345678";

            var samples = new (string Name, string Input, string[] Forbidden)[]
            {
                ("bearer-auth",
                    "Authorization: Bearer " + ipc,
                    new[] { ipc }),
                ("ipc-env",
                    "CMNET_IPC_TOKEN=" + ipc,
                    new[] { ipc }),
                ("stratz-env",
                    "STRATZ_TOKEN=st_live_supersecrettokenvalue123456",
                    new[] { "st_live_supersecrettokenvalue123456" }),
                ("opendota-env",
                    "OPENDOTA_API_KEY=odota_secret_key_ABCDEF",
                    new[] { "odota_secret_key_ABCDEF" }),
                ("json-token",
                    "{\"token\":\"" + ipc + "\",\"ok\":true}",
                    new[] { ipc }),
                ("yaml-token",
                    "token: " + ipc + "\nrank: immortal",
                    new[] { ipc }),
                ("jwt",
                    "hdr " + jwt + " tail",
                    new[] { jwt }),
                ("hex-blob",
                    "leak " + ipc + " done",
                    new[] { ipc }),
                ("account-labeled",
                    "account_id=" + account + " hero=1",
                    new[] { account }),
                ("account-json",
                    "{\"account_id\": " + account + "}",
                    new[] { account }),
                ("pem-private",
                    "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQC\n-----END PRIVATE KEY-----\n",
                    new[] { "MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQC" }),
            };

            foreach (var (name, input, forbidden) in samples)
            {
                var outText = SecretRedactor.Redact(input);
                if (!outText.Contains(SecretRedactor.Placeholder, StringComparison.Ordinal))
                    Fail("redact-" + name, "missing placeholder: " + outText);
                else if (!SecretRedactor.LooksCleanOf(outText, forbidden))
                    Fail("redact-" + name, "secret leaked: " + outText);
                else
                    Pass("redact-" + name);
            }

            // Clean text must survive unchanged (no false wipe of ordinary words).
            const string clean = "hero_id=1 pick=Anti-Mage status=ok";
            var cleanOut = SecretRedactor.Redact(clean);
            if (cleanOut == clean) Pass("redact-clean-passthrough");
            else Fail("redact-clean-passthrough", cleanOut);

            // --- Paths + README ---
            var root = DiagnosticsPaths.EnsureLayout();
            if (!Directory.Exists(root)) Fail("ensure-layout", "root missing");
            else Pass("ensure-layout");
            if (!File.Exists(DiagnosticsPaths.ReadmePath())) Fail("readme", "missing");
            else
            {
                var readme = File.ReadAllText(DiagnosticsPaths.ReadmePath());
                if (readme.Contains("%APPDATA%\\cmnet", StringComparison.OrdinalIgnoreCase) &&
                    readme.Contains("preserved", StringComparison.OrdinalIgnoreCase))
                    Pass("readme");
                else
                    Fail("readme", "honesty text incomplete");
            }

            // --- Breadcrumb write with secrets in exception message ---
            var boom = new InvalidOperationException(
                "core fault CMNET_IPC_TOKEN=" + ipc + " account_id=" + account);
            var crashPath = CrashBreadcrumbs.Write("self-test", boom, isTerminating: false);
            if (string.IsNullOrEmpty(crashPath) || !File.Exists(crashPath))
            {
                Fail("breadcrumb-write", "no file");
            }
            else
            {
                var body = File.ReadAllText(crashPath);
                if (!SecretRedactor.LooksCleanOf(body, ipc, account))
                    Fail("breadcrumb-redacted", "leaked secret in " + crashPath);
                else if (!body.Contains(SecretRedactor.Placeholder, StringComparison.Ordinal))
                    Fail("breadcrumb-redacted", "no placeholder");
                else if (!body.Contains("Gate 3.4", StringComparison.Ordinal))
                    Fail("breadcrumb-redacted", "missing header");
                else
                    Pass("breadcrumb-redacted");

                // Copy sample into evidence (already redacted).
                var sampleDest = Path.Combine(dir, "sample-crash-redacted.txt");
                File.Copy(crashPath, sampleDest, overwrite: true);
                Pass("evidence-sample-crash");
            }

            // Uninstall contract smoke (static): NSIS must not delete APPDATA\cmnet.
            var nsi = FindNsisScript();
            if (nsi == null)
            {
                Fail("nsis-preserve-contract", "cmnet-native.nsi not found");
            }
            else
            {
                var nsiText = File.ReadAllText(nsi);
                var mentionsPreserve = nsiText.Contains("%APPDATA%\\cmnet", StringComparison.OrdinalIgnoreCase)
                    || nsiText.Contains("APPDATA", StringComparison.OrdinalIgnoreCase);
                var deletesAppData = nsiText.Contains("RMDir /r \"$APPDATA\\cmnet\"", StringComparison.OrdinalIgnoreCase)
                    || nsiText.Contains("RMDir /r \"$APPDATA\\cmnet\"", StringComparison.Ordinal)
                    || nsiText.Contains("Delete \"$APPDATA\\cmnet", StringComparison.OrdinalIgnoreCase);
                // Also catch expanded forms without escapes.
                if (nsiText.Contains("RMDir /r \"$APPDATA\\cmnet\"", StringComparison.OrdinalIgnoreCase))
                    deletesAppData = true;
                if (nsiText.IndexOf("RMDir /r \"$APPDATA\\cmnet\"", StringComparison.OrdinalIgnoreCase) >= 0)
                    deletesAppData = true;

                // Safer scan: any RMDir targeting APPDATA cmnet.
                foreach (var line in nsiText.Split('\n'))
                {
                    var t = line.Trim();
                    if (t.StartsWith(";", StringComparison.Ordinal)) continue;
                    if (t.Contains("RMDir", StringComparison.OrdinalIgnoreCase) &&
                        t.Contains("APPDATA", StringComparison.OrdinalIgnoreCase) &&
                        t.Contains("cmnet", StringComparison.OrdinalIgnoreCase))
                        deletesAppData = true;
                    if (t.Contains("Delete", StringComparison.OrdinalIgnoreCase) &&
                        t.Contains("APPDATA", StringComparison.OrdinalIgnoreCase) &&
                        t.Contains("cmnet", StringComparison.OrdinalIgnoreCase) &&
                        !t.StartsWith(";", StringComparison.Ordinal))
                        deletesAppData = true;
                }

                if (!mentionsPreserve)
                    Fail("nsis-preserve-contract", "no APPDATA\\cmnet preserve comment/link");
                else if (deletesAppData)
                    Fail("nsis-preserve-contract", "NSIS deletes %APPDATA%\\cmnet");
                else
                    Pass("nsis-preserve-contract");
            }
        }
        catch (Exception ex)
        {
            failed++;
            log.AppendLine("FAIL  harness — " + ex);
        }

        log.AppendLine(failed == 0
            ? "RESULT OK — Gate 3.4 diagnostics fixtures"
            : "RESULT FAIL — " + failed + " assertion(s)");
        Console.Out.Write(log.ToString());
        File.WriteAllText(Path.Combine(dir, "self-test-diagnostics.txt"), log.ToString());
        File.WriteAllText(Path.Combine(dir, "checks.txt"), log.ToString());
        return Task.FromResult(failed == 0 ? 0 : 1);
    }

    private static string ResolveEvidence(string? arg)
    {
        if (!string.IsNullOrWhiteSpace(arg))
        {
            Directory.CreateDirectory(arg);
            return Path.GetFullPath(arg);
        }

        var candidates = new[]
        {
            Path.Combine(Environment.CurrentDirectory, "docs", "_gate0", "gate3-diagnostics-wip"),
            Path.Combine(AppContext.BaseDirectory, "docs", "_gate0", "gate3-diagnostics-wip"),
            Path.GetFullPath(Path.Combine(AppContext.BaseDirectory, "..", "..", "..", "..", "docs", "_gate0", "gate3-diagnostics-wip")),
        };
        foreach (var c in candidates)
        {
            var parent = Path.GetDirectoryName(c);
            if (parent != null && Directory.Exists(parent))
            {
                Directory.CreateDirectory(c);
                return Path.GetFullPath(c);
            }
        }

        var fallback = Path.Combine(Environment.CurrentDirectory, "docs", "_gate0", "gate3-diagnostics-wip");
        Directory.CreateDirectory(fallback);
        return Path.GetFullPath(fallback);
    }

    private static string? FindNsisScript()
    {
        var candidates = new[]
        {
            Path.Combine(Environment.CurrentDirectory, "native", "installer", "cmnet-native.nsi"),
            Path.GetFullPath(Path.Combine(AppContext.BaseDirectory, "..", "..", "..", "installer", "cmnet-native.nsi")),
            Path.GetFullPath(Path.Combine(AppContext.BaseDirectory, "..", "..", "..", "..", "native", "installer", "cmnet-native.nsi")),
        };
        foreach (var c in candidates)
            if (File.Exists(c)) return c;
        return null;
    }
}
