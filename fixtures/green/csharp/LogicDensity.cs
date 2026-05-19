namespace Demo;

public class Dense
{
    public string Value(string input)
    {
        var text = string.IsNullOrWhiteSpace(input) ? "ok" : input.Trim();
        return text.ToUpperInvariant();
    }
}
