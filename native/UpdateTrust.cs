using System;
using System.IO;
using System.Security.Cryptography;

namespace Cmnet.Native;

/// <summary>
/// Fail-closed trust material for native updates. Without a configured public
/// key, signature verification cannot succeed and apply stays disabled —
/// never invent or silently skip trust.
/// </summary>
internal static class UpdateTrust
{
    public const string EnvPublicKeyPem = "CMNET_UPDATE_PUBLIC_KEY_PEM";
    public const string EnvPublicKeyFile = "CMNET_UPDATE_PUBLIC_KEY_FILE";
    public const string SiblingTrustFileName = "update-trust.pem";

    public sealed record Material(string Source, string KeyIdHint, string Pem);

    /// <summary>
    /// Resolve trust material. Returns null when nothing is configured (fail-closed).
    /// </summary>
    public static Material? TryLoad(string? explicitPemPath = null)
    {
        if (!string.IsNullOrWhiteSpace(explicitPemPath) && File.Exists(explicitPemPath))
            return FromFile(explicitPemPath, "explicit:" + explicitPemPath);

        var envPem = Environment.GetEnvironmentVariable(EnvPublicKeyPem);
        if (!string.IsNullOrWhiteSpace(envPem))
            return new Material(EnvPublicKeyPem, "env-pem", NormalizePem(envPem));

        var envFile = Environment.GetEnvironmentVariable(EnvPublicKeyFile);
        if (!string.IsNullOrWhiteSpace(envFile) && File.Exists(envFile))
            return FromFile(envFile, EnvPublicKeyFile + ":" + envFile);

        try
        {
            var sibling = Path.Combine(AppContext.BaseDirectory, SiblingTrustFileName);
            if (File.Exists(sibling))
                return FromFile(sibling, "sibling:" + SiblingTrustFileName);
        }
        catch { /* unpackaged / restricted */ }

        return null;
    }

    public static bool VerifyRsaSha256(Material material, byte[] canonical, string signatureBase64, out string detail)
    {
        detail = "";
        try
        {
            var sig = Convert.FromBase64String(signatureBase64);
            using var rsa = RSA.Create();
            rsa.ImportFromPem(material.Pem);
            var ok = rsa.VerifyData(canonical, sig, HashAlgorithmName.SHA256, RSASignaturePadding.Pkcs1);
            detail = ok
                ? $"Подпись rsa-sha256 проверена ({material.Source})."
                : $"Подпись не совпала с ключом из {material.Source}.";
            return ok;
        }
        catch (Exception ex)
        {
            detail = "Ошибка проверки подписи: " + ex.Message;
            return false;
        }
    }

    /// <summary>
    /// How a real production cert/key plugs in later (documented for NOTES + UI).
    /// </summary>
    public static string PlugInInstructionsRu() =>
        "Продакшен-ключ (позже): положите публичный RSA PEM рядом с Cmnet.Native.exe как " +
        SiblingTrustFileName + ", или задайте " + EnvPublicKeyFile + " / " + EnvPublicKeyPem +
        ". Подпись манифеста — rsa-sha256 PKCS#1 по канонической строке schema/channel/version/sha256/fileName/url. " +
        "Authenticode-сертификат для NSIS — отдельный ops-шаг (не путать с ключом манифеста). " +
        "Без ключа применение обновлений остаётся fail-closed.";

    private static Material FromFile(string path, string source)
    {
        var pem = NormalizePem(File.ReadAllText(path));
        return new Material(source, Path.GetFileName(path), pem);
    }

    private static string NormalizePem(string raw)
    {
        var t = raw.Replace("\\n", "\n", StringComparison.Ordinal).Trim();
        if (!t.Contains("BEGIN", StringComparison.Ordinal))
            throw new InvalidOperationException("Public key PEM missing BEGIN marker.");
        return t;
    }
}
