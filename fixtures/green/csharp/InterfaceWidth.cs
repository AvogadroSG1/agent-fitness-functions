namespace Demo;

public class NarrowApi
{
    public string Execute(string value)
    {
        var text = string.IsNullOrWhiteSpace(value) ? "unknown" : value.Trim();
        return text.ToUpperInvariant();
    }
}
