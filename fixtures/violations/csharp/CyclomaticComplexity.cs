namespace Demo;

public class RouteScore
{
    public int Score(string kind, int retries, bool urgent)
    {
        var score = 0;
        if (kind == "create") score++;
        if (kind == "update") score++;
        if (kind == "delete") score++;
        if (kind == "manual") score++;
        if (kind == "batch") score++;
        if (kind == "sync") score++;
        if (retries > 0) score++;
        if (retries > 1) score++;
        if (retries > 2) score++;
        if (urgent) score++;
        return score;
    }
}
