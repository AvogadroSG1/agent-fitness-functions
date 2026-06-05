using System.Text.Json;
using System.Text.Json.Serialization;
using Microsoft.Build.Locator;
using Microsoft.CodeAnalysis;
using Microsoft.CodeAnalysis.CSharp;
using Microsoft.CodeAnalysis.CSharp.Syntax;
using Microsoft.CodeAnalysis.MSBuild;

// Args: <file.cs> [--project <foo.csproj>]
string? projectPath = null;
string? file = null;
for (var i = 0; i < args.Length; i++)
{
    if (args[i] == "--project" && i + 1 < args.Length)
    {
        projectPath = args[++i];
    }
    else if (!args[i].StartsWith("--", StringComparison.Ordinal))
    {
        file = args[i];
    }
}
if (file is null)
{
    Console.Error.WriteLine("usage: calm-roslyn-analyzer <file.cs> [--project <foo.csproj>]");
    return 2;
}

var source = await File.ReadAllTextAsync(file);
var tree = CSharpSyntaxTree.ParseText(source, path: file);
var root = await tree.GetRootAsync();
SemanticModel semanticModel;
if (projectPath is not null)
{
    semanticModel = await ProjectSemanticModel(tree, file, projectPath);
}
else
{
    semanticModel = PlatformSemanticModel(tree);
}
var lineSpan = tree.GetLineSpan(root.FullSpan);
var usingDirectives = root.DescendantNodes().OfType<UsingDirectiveSyntax>().ToList();
var publicMethods = 0;
var functions = new List<FunctionMetric>();

var functionNodes = root.DescendantNodes()
    .Where(node => node is BaseMethodDeclarationSyntax or LocalFunctionStatementSyntax or AccessorDeclarationSyntax)
    .ToList();

foreach (var functionNode in functionNodes)
{
    var isPublic = IsPublicFunction(functionNode);
    if (isPublic)
    {
        publicMethods++;
    }

    var span = tree.GetLineSpan(functionNode.FullSpan);
    functions.Add(new FunctionMetric
    {
        Name = FunctionName(functionNode),
        CyclomaticComplexity = Complexity(functionNode),
        IsPublic = isPublic,
        LOC = span.EndLinePosition.Line - span.StartLinePosition.Line + 1,
    });
}

var importMetric = ImportMetric(usingDirectives, root, semanticModel);
var totalLOC = lineSpan.EndLinePosition.Line - lineSpan.StartLinePosition.Line + 1;
var logicLOC = source.Split('\n').Count(IsLogicLine);
var result = new AnalysisResult
{
    CALMNode = CALMNode(root, file),
    Language = "csharp",
    File = file,
    Functions = functions,
    ModuleMetric = new ModuleMetric
    {
        PublicMethods = publicMethods,
        TotalLOC = totalLOC,
        PrivateLOC = Math.Max(0, totalLOC - functions.Where(function => function.IsPublic).Sum(function => function.LOC)),
        AverageLOCPerPublicMethod = publicMethods == 0 ? 1 : (double)logicLOC / publicMethods,
    },
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

static async Task<SemanticModel> ProjectSemanticModel(SyntaxTree tree, string filePath, string projectPath)
{
    MSBuildLocator.RegisterDefaults();
    using var workspace = MSBuildWorkspace.Create();
    var project = await workspace.OpenProjectAsync(projectPath);
    var compilation = await project.GetCompilationAsync();
    if (compilation is null)
    {
        return PlatformSemanticModel(tree);
    }
    var normalizedFile = Path.GetFullPath(filePath);
    var document = project.Documents.FirstOrDefault(d =>
        string.Equals(Path.GetFullPath(d.FilePath ?? ""), normalizedFile, StringComparison.OrdinalIgnoreCase));
    if (document is not null)
    {
        var updatedDoc = document.WithSyntaxRoot(await tree.GetRootAsync());
        var updatedCompilation = await updatedDoc.Project.GetCompilationAsync();
        if (updatedCompilation is not null)
        {
            return updatedCompilation.GetSemanticModel(tree);
        }
    }
    return compilation.GetSemanticModel(tree);
}

static SemanticModel PlatformSemanticModel(SyntaxTree tree)
{
    var trustedPlatformAssemblies = AppContext.GetData("TRUSTED_PLATFORM_ASSEMBLIES") as string;
    var references = (trustedPlatformAssemblies ?? "")
        .Split(Path.PathSeparator, StringSplitOptions.RemoveEmptyEntries)
        .Select(path => MetadataReference.CreateFromFile(path))
        .Cast<MetadataReference>()
        .ToList();
    var compilation = CSharpCompilation.Create(
        "CalmRoslynAnalysis",
        [tree],
        references,
        new CSharpCompilationOptions(OutputKind.DynamicallyLinkedLibrary));
    return compilation.GetSemanticModel(tree);
}

static int Complexity(SyntaxNode functionNode)
{
    var complexity = 1;
    foreach (var node in functionNode.DescendantNodes(descendIntoChildren: node => node == functionNode || !IsFunctionNode(node)))
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

static bool IsFunctionNode(SyntaxNode node) => node is BaseMethodDeclarationSyntax or LocalFunctionStatementSyntax or AccessorDeclarationSyntax;

static string FunctionName(SyntaxNode functionNode) => functionNode switch
{
    MethodDeclarationSyntax method => method.Identifier.ValueText,
    ConstructorDeclarationSyntax constructor => constructor.Identifier.ValueText,
    DestructorDeclarationSyntax destructor => "~" + destructor.Identifier.ValueText,
    OperatorDeclarationSyntax operatorDeclaration => "operator " + operatorDeclaration.OperatorToken.ValueText,
    ConversionOperatorDeclarationSyntax conversion => "operator " + conversion.Type,
    LocalFunctionStatementSyntax localFunction => localFunction.Identifier.ValueText,
    AccessorDeclarationSyntax accessor => AccessorName(accessor),
    _ => functionNode.Kind().ToString(),
};

static string AccessorName(AccessorDeclarationSyntax accessor)
{
    var memberName = accessor.Parent?.Parent switch
    {
        PropertyDeclarationSyntax property => property.Identifier.ValueText,
        IndexerDeclarationSyntax => "this[]",
        EventDeclarationSyntax eventDeclaration => eventDeclaration.Identifier.ValueText,
        _ => "accessor",
    };
    return memberName + "." + accessor.Keyword.ValueText;
}

static bool IsPublicFunction(SyntaxNode functionNode)
{
    if (functionNode.FirstAncestorOrSelf<InterfaceDeclarationSyntax>() is not null)
    {
        return true;
    }
    return functionNode switch
    {
        BaseMethodDeclarationSyntax method => method.Modifiers.Any(SyntaxKind.PublicKeyword),
        LocalFunctionStatementSyntax localFunction => localFunction.Modifiers.Any(SyntaxKind.PublicKeyword),
        AccessorDeclarationSyntax accessor => IsPublicAccessor(accessor),
        _ => false,
    };
}

static bool IsPublicAccessor(AccessorDeclarationSyntax accessor)
{
    if (accessor.Modifiers.Any(SyntaxKind.PublicKeyword))
    {
        return true;
    }
    return accessor.Parent?.Parent switch
    {
        BasePropertyDeclarationSyntax property => property.Modifiers.Any(SyntaxKind.PublicKeyword),
        _ => false,
    };
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

static SemanticModel SemanticModel(SyntaxTree tree)
{
    var trustedPlatformAssemblies = AppContext.GetData("TRUSTED_PLATFORM_ASSEMBLIES") as string;
    var references = (trustedPlatformAssemblies ?? "")
        .Split(Path.PathSeparator, StringSplitOptions.RemoveEmptyEntries)
        .Select(path => MetadataReference.CreateFromFile(path))
        .Cast<MetadataReference>()
        .ToList();
    var compilation = CSharpCompilation.Create(
        "CalmRoslynAnalysis",
        [tree],
        references,
        new CSharpCompilationOptions(OutputKind.DynamicallyLinkedLibrary));
    return compilation.GetSemanticModel(tree);
}

static ImportMetric ImportMetric(IReadOnlyCollection<UsingDirectiveSyntax> usingDirectives, SyntaxNode root, SemanticModel semanticModel)
{
    var imports = usingDirectives
        .Select(u => new ImportEntry(
            DisplayName: u.Alias?.Name.Identifier.ValueText ?? u.Name?.ToString().Split('.').LastOrDefault() ?? "",
            NamespaceOrType: u.Name?.ToString() ?? "",
            Alias: u.Alias?.Name.Identifier.ValueText ?? ""))
        .Where(import => import.DisplayName.Length > 0)
        .ToList();
    var identifiers = root.DescendantNodes()
        .OfType<IdentifierNameSyntax>()
        .Where(identifier => !usingDirectives.Any(usingDirective => usingDirective.Span.Contains(identifier.SpanStart)))
        .ToList();
    var usedImports = imports
        .Where(import => IsImportUsed(import, identifiers, semanticModel))
        .ToHashSet();
    var unused = imports
        .Where(import => !usedImports.Contains(import))
        .Select(import => import.DisplayName)
        .ToList();
    return new ImportMetric
    {
        Total = imports.Count,
        Used = usedImports.Count,
        Unused = unused,
        DDC = Ratio(usedImports.Count, imports.Count),
    };
}

static bool IsImportUsed(ImportEntry import, IReadOnlyCollection<IdentifierNameSyntax> identifiers, SemanticModel semanticModel)
{
    if (import.Alias.Length > 0)
    {
        return identifiers.Any(identifier => identifier.Identifier.ValueText == import.Alias);
    }
    return identifiers.Any(identifier =>
    {
        var symbol = semanticModel.GetSymbolInfo(identifier).Symbol;
        var containingNamespace = symbol?.ContainingNamespace?.ToDisplayString() ?? "";
        return containingNamespace == import.NamespaceOrType ||
            containingNamespace.StartsWith(import.NamespaceOrType + ".", StringComparison.Ordinal);
    });
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
    [JsonPropertyName("module_metrics")]
    public ModuleMetric ModuleMetric { get; set; } = new();
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

sealed class ModuleMetric
{
    [JsonPropertyName("public_method_count")]
    public int PublicMethods { get; set; }
    [JsonPropertyName("total_loc")]
    public int TotalLOC { get; set; }
    [JsonPropertyName("private_loc")]
    public int PrivateLOC { get; set; }
    [JsonPropertyName("avg_loc_per_public_method")]
    public double AverageLOCPerPublicMethod { get; set; }
}

sealed class ImportMetric
{
    public int Total { get; set; }
    public int Used { get; set; }
    public List<string> Unused { get; set; } = [];
    public double DDC { get; set; }
}

sealed record ImportEntry(string DisplayName, string NamespaceOrType, string Alias);
