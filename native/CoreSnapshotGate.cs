using System;
using System.Text.Json.Nodes;

namespace Cmnet.Native;

// Version/revision gate shared by every WPF view. Old SSE updates must never
// overwrite the state returned by a more recent user action.
public sealed class CoreSnapshotGate
{
    private string currentSession = "";
    private long revision = -1;

    public bool TryAccept(JsonObject snapshot)
    {
        if (snapshot["schemaVersion"]?.GetValue<int>() != 1)
            throw new InvalidOperationException("Unsupported CMNET core API version. Update the native client and advisor.exe together.");
        string id = snapshot["sessionId"]?.GetValue<string>() ?? "";
        long next = snapshot["revision"]?.GetValue<long>() ?? -1;
        if (id.Length == 0 || next < 0)
            throw new InvalidOperationException("Invalid CMNET core state envelope");
        if (string.Equals(id, currentSession, StringComparison.Ordinal) && next <= revision)
            return false;
        currentSession = id;
        revision = next;
        return true;
    }

    // Exercised by --smoke-ui without creating a network process or user files.
    public static void SmokeTest()
    {
        var gate = new CoreSnapshotGate();
        static JsonObject State(string id, int rev) => new()
        {
            ["schemaVersion"] = 1, ["sessionId"] = id, ["revision"] = (long)rev
        };
        if (!gate.TryAccept(State("a", 1)) || gate.TryAccept(State("a", 1)) ||
            gate.TryAccept(State("a", 0)) || !gate.TryAccept(State("a", 2)) ||
            !gate.TryAccept(State("b", 1)))
            throw new InvalidOperationException("Core state revision gate failed its smoke test");
    }
}
