namespace Demo;

public class DeepApi
{
    public string Describe(string name)
    {
        var value = string.IsNullOrWhiteSpace(name) ? "unknown" : name.Trim();
        return value.ToUpperInvariant();
    }
}
