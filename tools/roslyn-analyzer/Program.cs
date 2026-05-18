using System.Text.Json;
using System.Text.Json.Serialization;
using Microsoft.CodeAnalysis;
using Microsoft.CodeAnalysis.CSharp;
using Microsoft.CodeAnalysis.CSharp.Syntax;

if (args.Length != 1)
{
    Console.Error.WriteLine("usage: calm-roslyn-analyzer <file.cs>");
    return 2;
}

var file = args[0];
var source = await File.ReadAllTextAsync(file);
var tree = CSharpSyntaxTree.ParseText(source, path: file);
var root = await tree.GetRootAsync();
var lineSpan = tree.GetLineSpan(root.FullSpan);
var usingDirectives = root.DescendantNodes().OfType<UsingDirectiveSyntax>().ToList();
var publicMethods = 0;
var functions = new List<FunctionMetric>();

foreach (var method in root.DescendantNodes().OfType<MethodDeclarationSyntax>())
{
    var isPublic = method.Modifiers.Any(SyntaxKind.PublicKeyword);
    if (isPublic)
    {
        publicMethods++;
    }

    var span = tree.GetLineSpan(method.FullSpan);
    functions.Add(new FunctionMetric
    {
        Name = method.Identifier.ValueText,
        CyclomaticComplexity = Complexity(method),
        IsPublic = isPublic,
        LOC = span.EndLinePosition.Line - span.StartLinePosition.Line + 1,
    });
}

var importMetric = ImportMetric(usingDirectives, root);
var totalLOC = lineSpan.EndLinePosition.Line - lineSpan.StartLinePosition.Line + 1;
var logicLOC = source.Split('\n').Count(IsLogicLine);
var result = new AnalysisResult
{
    CALMNode = CALMNode(root, file),
    Language = "csharp",
    File = file,
    Functions = functions,
    FileMetric = new FileMetric
    {
        TotalLOC = totalLOC,
        LogicLOC = logicLOC,
        PublicMethods = publicMethods,
        LDR = Ratio(logicLOC, totalLOC),
    },
    Imports = importMetric,
};

var json = JsonSerializer.Serialize(result, new JsonSerializerOptions
{
    PropertyNamingPolicy = JsonNamingPolicy.SnakeCaseLower,
    WriteIndented = true,
});
Console.WriteLine(json);
return 0;

static int Complexity(MethodDeclarationSyntax method)
{
    var complexity = 1;
    foreach (var node in method.DescendantNodes())
    {
        complexity += node switch
        {
            IfStatementSyntax => 1,
            ForStatementSyntax => 1,
            ForEachStatementSyntax => 1,
            WhileStatementSyntax => 1,
            DoStatementSyntax => 1,
            CaseSwitchLabelSyntax => 1,
            ConditionalExpressionSyntax => 1,
            BinaryExpressionSyntax binary when binary.IsKind(SyntaxKind.LogicalAndExpression) || binary.IsKind(SyntaxKind.LogicalOrExpression) => 1,
            _ => 0,
        };
    }
    return complexity;
}

static string CALMNode(SyntaxNode root, string file)
{
    var namespaceNode = root.DescendantNodes().OfType<BaseNamespaceDeclarationSyntax>().FirstOrDefault();
    if (namespaceNode is not null)
    {
        return namespaceNode.Name.ToString();
    }
    var classNode = root.DescendantNodes().OfType<ClassDeclarationSyntax>().FirstOrDefault();
    return classNode?.Identifier.ValueText ?? Path.GetFileNameWithoutExtension(file);
}

static ImportMetric ImportMetric(IReadOnlyCollection<UsingDirectiveSyntax> usingDirectives, SyntaxNode root)
{
    var names = usingDirectives
        .Select(u => u.Alias?.Name.Identifier.ValueText ?? u.Name?.ToString().Split('.').LastOrDefault() ?? "")
        .Where(name => name.Length > 0)
        .ToList();
    var identifiers = root.DescendantNodes().OfType<IdentifierNameSyntax>().Select(i => i.Identifier.ValueText).ToHashSet();
    var used = names.Count(identifiers.Contains);
    var unused = names.Where(name => !identifiers.Contains(name)).ToList();
    return new ImportMetric
    {
        Total = names.Count,
        Used = used,
        Unused = unused,
        DDC = Ratio(used, names.Count),
    };
}

static bool IsLogicLine(string line)
{
    var trimmed = line.Trim();
    if (trimmed.Length == 0 ||
        trimmed.StartsWith("//", StringComparison.Ordinal) ||
        trimmed.StartsWith("using ", StringComparison.Ordinal) ||
        trimmed.StartsWith("namespace ", StringComparison.Ordinal) ||
        trimmed.StartsWith("public class ", StringComparison.Ordinal) ||
        trimmed is "{" or "}")
    {
        return false;
    }
    return true;
}

static double Ratio(int numerator, int denominator) => denominator == 0 ? 1 : (double)numerator / denominator;

sealed class AnalysisResult
{
    [JsonPropertyName("calm_node")]
    public string CALMNode { get; set; } = "";
    public string Language { get; set; } = "";
    public string File { get; set; } = "";
    public List<FunctionMetric> Functions { get; set; } = [];
    [JsonPropertyName("file_metrics")]
    public FileMetric FileMetric { get; set; } = new();
    [JsonPropertyName("import_metrics")]
    public ImportMetric Imports { get; set; } = new();
}

sealed class FunctionMetric
{
    public string Name { get; set; } = "";
    public int CyclomaticComplexity { get; set; }
    public bool IsPublic { get; set; }
    public int LOC { get; set; }
}

sealed class FileMetric
{
    public int TotalLOC { get; set; }
    public int LogicLOC { get; set; }
    public int PublicMethods { get; set; }
    public double LDR { get; set; }
}

sealed class ImportMetric
{
    public int Total { get; set; }
    public int Used { get; set; }
    public List<string> Unused { get; set; } = [];
    public double DDC { get; set; }
}
