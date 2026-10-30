using System;
using System.Collections.Generic;
using System.Text.Json.Nodes;

namespace Cmnet.Native;

public static class J
{
    public static JsonNode? At(this JsonNode? n, params string[] keys)
    {
        foreach (var key in keys)
        {
            if (n is not JsonObject obj || !obj.TryGetPropertyValue(key, out n))
                return null;
        }
        return n;
    }
    public static string S(this JsonNode? n, string fallback = "")
    {
        try { return n?.ToString() ?? fallback; } catch { return fallback; }
    }
    public static bool B(this JsonNode? n)
    {
        try { return n?.GetValue<bool>() ?? false; } catch { return false; }
    }
    public static int I(this JsonNode? n)
    {
        try { return n?.GetValue<int>() ?? 0; } catch { return 0; }
    }
    public static long L(this JsonNode? n)
    {
        try { return n?.GetValue<long>() ?? 0; } catch { return 0; }
    }
    public static double D(this JsonNode? n)
    {
        try { return n?.GetValue<double>() ?? 0; } catch { return 0; }
    }
    public static IEnumerable<JsonNode?> Items(this JsonNode? n) => (n as JsonArray) ?? new JsonArray();
}
