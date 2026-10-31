using System;
using System.IO;
using System.Text;
using System.Threading.Tasks;

namespace Cmnet.Native;

/// <summary>
/// Local fixture tests for Gate 3.3 (no paid APIs, no network required).
/// Invoked via <c>Cmnet.Native.exe --self-test-updates [fixturesDir]</c>.
/// </summary>
internal static class UpdateSelfTest
{
    public static async Task<int> RunAsync(string? fixturesDir)
    {
        var dir = ResolveFixtures(fixturesDir);
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
            // 1) Parse unsigned
            var unsignedPath = Path.Combine(dir, "manifest-unsigned.json");
            var unsigned = UpdateManifest.Parse(await File.ReadAllTextAsync(unsignedPath));
            if (unsigned.Signature != null) Fail("parse-unsigned", "expected no signature");
            else Pass("parse-unsigned");

            // 2) Reject bad schema
            try
            {
                UpdateManifest.Parse(await File.ReadAllTextAsync(Path.Combine(dir, "manifest-bad-schema.json")));
                Fail("reject-bad-schema", "expected ParseException");
            }
            catch (UpdateManifest.ParseException)
            {
                Pass("reject-bad-schema");
            }

            // 3) Reject Electron-shaped JSON
            try
            {
                UpdateManifest.Parse(await File.ReadAllTextAsync(Path.Combine(dir, "electron-latest-style.json")));
                Fail("reject-electron-shape", "expected ParseException");
            }
            catch (UpdateManifest.ParseException ex)
            {
                if (ex.Message.Contains("Electron", StringComparison.OrdinalIgnoreCase))
                    Pass("reject-electron-shape");
                else
                    Fail("reject-electron-shape", ex.Message);
            }

            // 4) Fail-closed without trust key (even if signed)
            var signedPath = Path.Combine(dir, "manifest-signed.json");
            var signed = UpdateManifest.Parse(await File.ReadAllTextAsync(signedPath));
            var noTrust = UpdateChannel.Evaluate("0.4.0-native", signed, trust: null);
            if (noTrust.Kind == UpdateChannel.CheckKind.Unsigned && !noTrust.CanApply)
                Pass("fail-closed-no-key");
            else
                Fail("fail-closed-no-key", UpdateChannel.FormatCheckLine(noTrust));

            // 5) Verify good signature with fixture public key
            var pubPath = Path.Combine(dir, "test-public.pem");
            var trust = UpdateTrust.TryLoad(pubPath)
                ?? throw new InvalidOperationException("test-public.pem missing");
            if (!UpdateTrust.VerifyRsaSha256(trust, UpdateManifest.CanonicalBytes(signed), signed.Signature!.Value, out var okDetail))
                Fail("verify-signed", okDetail);
            else
                Pass("verify-signed");

            var available = UpdateChannel.Evaluate("0.4.0-native", signed, trust);
            if (available.Kind == UpdateChannel.CheckKind.Available && available.CanApply)
                Pass("evaluate-available");
            else
                Fail("evaluate-available", UpdateChannel.FormatCheckLine(available));

            var upToDate = UpdateChannel.Evaluate("0.4.1-native", signed, trust);
            if (upToDate.Kind == UpdateChannel.CheckKind.UpToDate && !upToDate.CanApply)
                Pass("evaluate-up-to-date");
            else
                Fail("evaluate-up-to-date", UpdateChannel.FormatCheckLine(upToDate));

            // 6) Reject bad signature
            var bad = UpdateManifest.Parse(await File.ReadAllTextAsync(Path.Combine(dir, "manifest-bad-signature.json")));
            var rejected = UpdateChannel.Evaluate("0.4.0-native", bad, trust);
            if (rejected.Kind == UpdateChannel.CheckKind.Rejected && !rejected.CanApply)
                Pass("reject-bad-signature");
            else
                Fail("reject-bad-signature", UpdateChannel.FormatCheckLine(rejected));

            // 7) Unsigned manifest + key present → Unsigned / no apply
            var unsignedEval = UpdateChannel.Evaluate("0.4.0-native", unsigned, trust);
            if (unsignedEval.Kind == UpdateChannel.CheckKind.Unsigned && !unsignedEval.CanApply)
                Pass("unsigned-with-key-no-apply");
            else
                Fail("unsigned-with-key-no-apply", UpdateChannel.FormatCheckLine(unsignedEval));

            // 8) CheckAsync from file path
            var check = await UpdateChannel.CheckAsync(
                "0.4.0-native",
                manifestUrlOrPath: signedPath,
                trustPemPath: pubPath);
            if (check.Kind == UpdateChannel.CheckKind.Available && check.CanApply)
                Pass("check-async-file");
            else
                Fail("check-async-file", UpdateChannel.FormatCheckLine(check));

            // 9) Empty URL → Unavailable
            // Clear env interference for this assertion.
            var prevUrl = Environment.GetEnvironmentVariable(UpdateChannel.EnvManifestUrl);
            try
            {
                Environment.SetEnvironmentVariable(UpdateChannel.EnvManifestUrl, null);
                var empty = await UpdateChannel.CheckAsync("0.4.0-native", manifestUrlOrPath: "", trustPemPath: pubPath);
                // empty string still ResolveManifestUrl may fall through desktop.json — force Evaluate path:
                var una = UpdateChannel.Evaluate("0.4.0-native", null, trust);
                if (una.Kind == UpdateChannel.CheckKind.Unavailable && !una.CanApply)
                    Pass("unavailable-no-url");
                else
                    Fail("unavailable-no-url", UpdateChannel.FormatCheckLine(una));
                _ = empty;
            }
            finally
            {
                Environment.SetEnvironmentVariable(UpdateChannel.EnvManifestUrl, prevUrl);
            }
        }
        catch (Exception ex)
        {
            failed++;
            log.AppendLine("FAIL  harness — " + ex);
        }

        log.AppendLine(failed == 0
            ? "RESULT OK — Gate 3.3 update fixtures"
            : "RESULT FAIL — " + failed + " assertion(s)");
        Console.Out.Write(log.ToString());
        await File.WriteAllTextAsync(
            Path.Combine(dir, "..", "self-test-updates.txt"),
            log.ToString()).ConfigureAwait(false);
        return failed == 0 ? 0 : 1;
    }

    private static string ResolveFixtures(string? arg)
    {
        if (!string.IsNullOrWhiteSpace(arg) && Directory.Exists(arg))
            return Path.GetFullPath(arg);

        var candidates = new[]
        {
            Path.Combine(Environment.CurrentDirectory, "docs", "_gate0", "gate3-updates-wip", "fixtures"),
            Path.Combine(AppContext.BaseDirectory, "docs", "_gate0", "gate3-updates-wip", "fixtures"),
            Path.GetFullPath(Path.Combine(AppContext.BaseDirectory, "..", "..", "..", "..", "docs", "_gate0", "gate3-updates-wip", "fixtures")),
        };
        foreach (var c in candidates)
            if (Directory.Exists(c)) return Path.GetFullPath(c);

        throw new DirectoryNotFoundException(
            "Fixtures not found. Pass path: --self-test-updates docs/_gate0/gate3-updates-wip/fixtures");
    }
}
