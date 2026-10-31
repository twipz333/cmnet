using System;
using System.Globalization;
using System.Text;
using System.Text.Json;
using System.Text.Json.Nodes;

namespace Cmnet.Native;

/// <summary>
/// Versioned native update manifest (Gate 3.3). Distinct from Electron
/// electron-updater <c>latest.yml</c> / <c>guest.yml</c> — do not publish native
/// artifacts into that channel or split GitHub Releases the way host+guest once did.
/// </summary>
internal static class UpdateManifest
{
    public const string SchemaId = "cmnet-native-update/v1";
    public const string ChannelNative = "native";
    public const string SignatureAlgRsaSha256 = "rsa-sha256";
    public const string ArtifactKindNsis = "nsis-setup";
    public const string ArtifactKindZip = "portable-zip";

    public sealed record Artifact(
        string Kind,
        string FileName,
        string Url,
        string Sha256,
        long SizeBytes);

    public sealed record Signature(string Alg, string KeyId, string Value);

    public sealed record Document(
        string Schema,
        string Channel,
        string Version,
        string Informational,
        bool Released,
        DateTimeOffset? PublishedUtc,
        string? MinClientVersion,
        string? Notes,
        Artifact Artifact,
        Signature? Signature);

    public sealed class ParseException : Exception
    {
        public ParseException(string message) : base(message) { }
    }

    public static Document Parse(string json)
    {
        JsonObject root;
        try
        {
            root = JsonNode.Parse(json) as JsonObject
                ?? throw new ParseException("Manifest root must be a JSON object.");
        }
        catch (JsonException ex)
        {
            throw new ParseException("Manifest is not valid JSON: " + ex.Message);
        }

        // Reject Electron latest.yml-shaped JSON early (honesty: wrong channel).
        if (root.ContainsKey("sha512") && root.ContainsKey("path") && !root.ContainsKey("schema"))
            throw new ParseException(
                "Looks like Electron electron-updater metadata (sha512/path), not " + SchemaId +
                ". Native update channel is separate; desktop.yml stays untouched.");

        var schema = ReqString(root, "schema");
        if (!string.Equals(schema, SchemaId, StringComparison.Ordinal))
            throw new ParseException($"Unsupported schema '{schema}'. Expected '{SchemaId}'.");

        var channel = ReqString(root, "channel");
        if (!string.Equals(channel, ChannelNative, StringComparison.Ordinal))
            throw new ParseException($"Unsupported channel '{channel}'. Expected '{ChannelNative}'.");

        var version = ReqString(root, "version");
        if (!IsLooseSemVer(version))
            throw new ParseException($"Invalid version '{version}'.");

        var informational = OptString(root, "informational") ?? version;
        var released = root["released"]?.GetValue<bool>() ?? false;
        DateTimeOffset? published = null;
        var publishedRaw = OptString(root, "publishedUtc");
        if (!string.IsNullOrWhiteSpace(publishedRaw))
        {
            if (!DateTimeOffset.TryParse(publishedRaw, CultureInfo.InvariantCulture,
                    DateTimeStyles.RoundtripKind, out var dto))
                throw new ParseException("publishedUtc is not a valid timestamp.");
            published = dto;
        }

        var artifactNode = root["artifact"] as JsonObject
            ?? throw new ParseException("Missing artifact object.");
        var kind = ReqString(artifactNode, "kind");
        if (kind is not (ArtifactKindNsis or ArtifactKindZip))
            throw new ParseException($"Unsupported artifact.kind '{kind}'.");

        var fileName = ReqString(artifactNode, "fileName");
        var url = ReqString(artifactNode, "url");
        var sha256 = ReqString(artifactNode, "sha256").ToLowerInvariant();
        if (sha256.Length != 64 || !IsHex(sha256))
            throw new ParseException("artifact.sha256 must be 64 lowercase hex chars.");
        var size = artifactNode["sizeBytes"]?.GetValue<long>()
            ?? throw new ParseException("artifact.sizeBytes is required.");
        if (size <= 0)
            throw new ParseException("artifact.sizeBytes must be positive.");

        Signature? signature = null;
        if (root["signature"] is JsonObject sigObj)
        {
            var alg = ReqString(sigObj, "alg");
            if (!string.Equals(alg, SignatureAlgRsaSha256, StringComparison.OrdinalIgnoreCase))
                throw new ParseException($"Unsupported signature.alg '{alg}'. Expected '{SignatureAlgRsaSha256}'.");
            var keyId = ReqString(sigObj, "keyId");
            var value = ReqString(sigObj, "value");
            try { _ = Convert.FromBase64String(value); }
            catch (FormatException)
            {
                throw new ParseException("signature.value is not valid Base64.");
            }
            signature = new Signature(alg.ToLowerInvariant(), keyId, value);
        }

        return new Document(
            schema,
            channel,
            version,
            informational,
            released,
            published,
            OptString(root, "minClientVersion"),
            OptString(root, "notes"),
            new Artifact(kind, fileName, url, sha256, size),
            signature);
    }

    /// <summary>
    /// Canonical bytes covered by rsa-sha256. Stable, field-ordered, no JSON ambiguity.
    /// </summary>
    public static byte[] CanonicalBytes(Document doc)
    {
        var text =
            doc.Schema + "\n" +
            doc.Channel + "\n" +
            doc.Version + "\n" +
            doc.Artifact.Sha256.ToLowerInvariant() + "\n" +
            doc.Artifact.FileName + "\n" +
            doc.Artifact.Url + "\n";
        return Encoding.UTF8.GetBytes(text);
    }

    public static string CanonicalString(Document doc) => Encoding.UTF8.GetString(CanonicalBytes(doc));

    public static int CompareVersions(string a, string b)
    {
        static Version ParseLoose(string v)
        {
            var core = v.Split('-', 2)[0].Trim();
            if (!Version.TryParse(core, out var ver))
            {
                // Allow "0.4" → 0.4.0
                var parts = core.Split('.');
                if (parts.Length == 2 &&
                    int.TryParse(parts[0], out var maj) &&
                    int.TryParse(parts[1], out var min))
                    return new Version(maj, min, 0);
                return new Version(0, 0, 0);
            }
            return ver.Build < 0 ? new Version(ver.Major, ver.Minor, 0) : ver;
        }
        return ParseLoose(a).CompareTo(ParseLoose(b));
    }

    private static string ReqString(JsonObject obj, string key)
    {
        var s = OptString(obj, key);
        if (string.IsNullOrWhiteSpace(s))
            throw new ParseException($"Missing or empty '{key}'.");
        return s;
    }

    private static string? OptString(JsonObject obj, string key)
    {
        var n = obj[key];
        if (n == null || n.GetValueKind() == JsonValueKind.Null) return null;
        return n.GetValue<string>()?.Trim();
    }

    private static bool IsLooseSemVer(string v)
    {
        var core = v.Split('-', 2)[0];
        var parts = core.Split('.');
        if (parts.Length is < 2 or > 4) return false;
        foreach (var p in parts)
            if (!int.TryParse(p, NumberStyles.None, CultureInfo.InvariantCulture, out _))
                return false;
        return true;
    }

    private static bool IsHex(string s)
    {
        foreach (var c in s)
        {
            var ok = c is >= '0' and <= '9' or >= 'a' and <= 'f';
            if (!ok) return false;
        }
        return true;
    }
}
