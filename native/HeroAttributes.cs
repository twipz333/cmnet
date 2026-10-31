namespace Cmnet.Native;

// Static Dota attribute catalog for gallery filters only. Not win-rate or
// meta strength — display taxonomy so Str/Agi/Int/Uni chips work offline
// without inventing live statistics.
internal static class HeroAttributes
{
    public enum Attr { All, Str, Agi, Int, Uni }

    // Post-7.36 primary attributes for heroes present in visual fixtures /
    // common CM pools. Unknown IDs fall through as All (never hidden wrongly
    // as "wrong attribute").
    private static readonly Dictionary<int, Attr> Map = new()
    {
        [1] = Attr.Agi, [2] = Attr.Str, [3] = Attr.Uni, [4] = Attr.Agi,
        [5] = Attr.Int, [6] = Attr.Agi, [7] = Attr.Str, [8] = Attr.Agi,
        [9] = Attr.Uni, [10] = Attr.Agi, [11] = Attr.Agi, [12] = Attr.Agi,
        [13] = Attr.Int, [14] = Attr.Str, [15] = Attr.Agi, [16] = Attr.Uni,
        [17] = Attr.Int, [18] = Attr.Str, [19] = Attr.Str, [20] = Attr.Uni,
        [21] = Attr.Uni, [22] = Attr.Int, [23] = Attr.Str, [25] = Attr.Int,
        [26] = Attr.Int, [27] = Attr.Int, [28] = Attr.Str, [29] = Attr.Str,
        [30] = Attr.Int, [31] = Attr.Int, [32] = Attr.Agi, [33] = Attr.Uni,
        [34] = Attr.Int, [35] = Attr.Agi, [36] = Attr.Uni, [37] = Attr.Int,
        [38] = Attr.Uni, [39] = Attr.Int, [40] = Attr.Uni, [41] = Attr.Agi,
        [42] = Attr.Str, [43] = Attr.Uni, [44] = Attr.Agi, [45] = Attr.Int,
        [46] = Attr.Agi, [47] = Attr.Uni, [48] = Attr.Agi, [49] = Attr.Str,
        [50] = Attr.Uni, [51] = Attr.Str, [52] = Attr.Int, [53] = Attr.Int,
        [54] = Attr.Str, [55] = Attr.Uni, [56] = Attr.Agi, [57] = Attr.Str,
        [58] = Attr.Int, [59] = Attr.Str, [60] = Attr.Str, [61] = Attr.Uni,
        [62] = Attr.Agi, [63] = Attr.Agi, [64] = Attr.Int, [65] = Attr.Uni,
        [66] = Attr.Uni, [67] = Attr.Agi, [68] = Attr.Int, [69] = Attr.Str,
        [70] = Attr.Agi, [71] = Attr.Str, [72] = Attr.Agi, [73] = Attr.Str,
        [74] = Attr.Int, [75] = Attr.Int, [76] = Attr.Int, [77] = Attr.Uni,
        [78] = Attr.Uni, [79] = Attr.Int, [80] = Attr.Uni, [81] = Attr.Str,
        [82] = Attr.Agi, [83] = Attr.Str, [84] = Attr.Str, [85] = Attr.Str,
        [86] = Attr.Int, [87] = Attr.Int, [88] = Attr.Agi, [89] = Attr.Agi,
        [90] = Attr.Int, [91] = Attr.Uni, [92] = Attr.Uni, [93] = Attr.Agi,
        [94] = Attr.Agi, [95] = Attr.Agi, [96] = Attr.Str, [97] = Attr.Uni,
        [98] = Attr.Str, [99] = Attr.Str, [100] = Attr.Str, [101] = Attr.Int,
        [102] = Attr.Uni, [103] = Attr.Str, [104] = Attr.Str, [105] = Attr.Uni,
        [106] = Attr.Agi, [107] = Attr.Str, [108] = Attr.Str, [109] = Attr.Agi,
        [110] = Attr.Uni, [111] = Attr.Int, [112] = Attr.Int, [113] = Attr.Agi,
        [114] = Attr.Agi, [119] = Attr.Uni, [120] = Attr.Uni, [121] = Attr.Uni,
        [123] = Attr.Uni, [126] = Attr.Uni, [128] = Attr.Uni, [129] = Attr.Str,
        [135] = Attr.Str, [136] = Attr.Uni, [137] = Attr.Agi, [138] = Attr.Int,
        [145] = Attr.Agi
    };

    public static Attr Of(int heroId) =>
        Map.TryGetValue(heroId, out var attr) ? attr : Attr.All;

    public static bool Matches(int heroId, Attr filter) =>
        filter == Attr.All || Of(heroId) == filter || Of(heroId) == Attr.All;

    public static string Label(Attr attr) => attr switch
    {
        Attr.Str => "Сила",
        Attr.Agi => "Ловкость",
        Attr.Int => "Интеллект",
        Attr.Uni => "Универсал",
        _ => "Все"
    };

    public static string Short(Attr attr) => attr switch
    {
        Attr.Str => "Str",
        Attr.Agi => "Agi",
        Attr.Int => "Int",
        Attr.Uni => "Uni",
        _ => "Все"
    };
}
