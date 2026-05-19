using System;
using System.Collections.Generic;

namespace Demo;

public class RouteScore
{
    public int Score(string kind, int retries, bool urgent)
    {
        var scores = new Dictionary<string, int>
        {
            ["create"] = 1,
            ["update"] = 1,
            ["delete"] = 1,
            ["manual"] = 1,
            ["batch"] = 1,
            ["sync"] = 1,
        };
        var score = scores.GetValueOrDefault(kind, 0) + Math.Min(Math.Max(retries, 0), 3);
        return urgent ? score + 1 : score;
    }
}
