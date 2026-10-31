using System;
using System.Text;
using System.Text.RegularExpressions;

namespace Cmnet.Native;

/// <summary>
/// Gate 3.4 — strip secrets from crash/diagnostic text before it hits disk.
/// Local-first: no cloud. Best-effort patterns for tokens, API keys, account ids.
/// </summary>
internal static class SecretRedactor
{
    public const string Placeholder = "[REDACTED]";

    // Order matters: more specific patterns first.
    private static readonly (Regex Rx, string Label)[] Patterns =
    {
        // PEM private keys (entire block).
        (new Regex(
            @"-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z0-9 ]*PRIVATE KEY-----",
            RegexOptions.Compiled | RegexOptions.CultureInvariant), "pem"),

        // Authorization: Bearer / Basic …
        (new Regex(
            @"(?i)(Authorization\s*[:=]\s*)(Bearer\s+)?([^\s""']+)",
            RegexOptions.Compiled | RegexOptions.CultureInvariant), "auth"),

        // CMNET_IPC_TOKEN=… / STRATZ_TOKEN=… / OPENDOTA_API_KEY=…
        (new Regex(
            @"(?i)\b((?:CMNET_IPC_TOKEN|STRATZ[_-]?TOKEN|OPENDOTA[_-]?(?:API[_-]?)?KEY|API[_-]?KEY|ACCESS[_-]?TOKEN|SECRET[_-]?KEY|CLIENT[_-]?SECRET))\s*[=:]\s*[""']?([^\s""';,]+)",
            RegexOptions.Compiled | RegexOptions.CultureInvariant), "env"),

        // JSON "token" / "apiKey" / "api_key" / "ipcToken" / "password" values.
        (new Regex(
            @"(?i)(""(?:token|api[_-]?key|ipc[_-]?token|password|secret|authorization|bearer)""\s*:\s*"")([^""]*)("")",
            RegexOptions.Compiled | RegexOptions.CultureInvariant), "json"),

        // YAML-ish token: value lines common in config.yaml dumps.
        (new Regex(
            @"(?im)^(\s*(?:token|api[_-]?key|stratz[_-]?token|opendota[_-]?(?:api[_-]?)?key|password|secret)\s*:\s*)(\S+)",
            RegexOptions.Compiled | RegexOptions.CultureInvariant), "yaml"),

        // JWT-shaped blobs (three base64url segments).
        (new Regex(
            @"\beyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\b",
            RegexOptions.Compiled | RegexOptions.CultureInvariant), "jwt"),

        // Long hex tokens (≥32) often used as IPC/API secrets.
        (new Regex(
            @"(?i)\b([0-9a-f]{32,})\b",
            RegexOptions.Compiled | RegexOptions.CultureInvariant), "hex"),

        // Steam/account ids in labeled context (account_id=7656119… / "accountId": 123).
        (new Regex(
            @"(?i)(\b(?:account[_-]?id|steam[_-]?id|steamid64|player[_-]?id)\b\s*[=:]\s*""?)(\d{5,20})",
            RegexOptions.Compiled | RegexOptions.CultureInvariant), "account"),

        (new Regex(
            @"(?i)(""(?:account[_-]?id|steam[_-]?id|steamid64|player[_-]?id)""\s*:\s*)(\d{5,20})",
            RegexOptions.Compiled | RegexOptions.CultureInvariant), "account-json"),
    };

    public static string Redact(string? input)
    {
        if (string.IsNullOrEmpty(input)) return input ?? "";
        var s = input;
        foreach (var (rx, label) in Patterns)
        {
            s = rx.Replace(s, m => ReplaceMatch(m, label));
        }
        return s;
    }

    private static string ReplaceMatch(Match m, string label)
    {
        // Keep structural prefixes so logs stay readable; scrub the secret payload.
        return label switch
        {
            "pem" => Placeholder + "(pem)",
            "auth" => m.Groups[1].Value + (m.Groups[2].Success ? m.Groups[2].Value : "") + Placeholder,
            "env" => m.Groups[1].Value + "=" + Placeholder,
            "json" => m.Groups[1].Value + Placeholder + m.Groups[3].Value,
            "yaml" => m.Groups[1].Value + Placeholder,
            "jwt" => Placeholder + "(jwt)",
            "hex" => Placeholder + "(hex)",
            "account" => m.Groups[1].Value + Placeholder,
            "account-json" => m.Groups[1].Value + Placeholder,
            _ => Placeholder
        };
    }

    /// <summary>
    /// True when redacted text still appears to contain a raw secret sample (for tests).
    /// </summary>
    public static bool LooksCleanOf(string redacted, params string[] forbiddenSubstrings)
    {
        foreach (var f in forbiddenSubstrings)
        {
            if (string.IsNullOrEmpty(f)) continue;
            if (redacted.Contains(f, StringComparison.Ordinal)) return false;
        }
        return true;
    }

    public static string DescribeRules()
    {
        var sb = new StringBuilder();
        sb.AppendLine("Gate 3.4 redaction: PEM private keys, Authorization/Bearer,");
        sb.AppendLine("CMNET_IPC_TOKEN / STRATZ / OPENDOTA / API keys, JSON token fields,");
        sb.AppendLine("JWT blobs, ≥32-char hex, labeled account_id / steam_id.");
        sb.Append("Replacement marker: ").Append(Placeholder);
        return sb.ToString();
    }
}
