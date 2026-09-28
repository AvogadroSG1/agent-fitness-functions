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
var repositoryArg = args.FirstOrDefault(a => a == "--repository");
if (repositoryArg is not null)
{
	var repoIndex = Array.IndexOf(args, repositoryArg);
	if (repoIndex + 1 >= args.Length) { Console.Error.WriteLine("--repository requires a path"); return 2; }
	return await RepositoryGraph(args[repoIndex + 1]);
}
if (file is null)
{
	Console.Error.WriteLine("usage: calm-roslyn-analyzer <file.cs> [--project <foo.csproj>] | --repository <repo>");
	return 2;
}

var source = await File.ReadAllTextAsync(file);
SyntaxTree tree;
SemanticModel semanticModel;
if (projectPath is not null)
{
    (semanticModel, tree) = await ProjectSemanticModel(source, file, projectPath);
}
else
{
    tree = CSharpSyntaxTree.ParseText(source, path: file);
    semanticModel = PlatformSemanticModel(tree);
}
var root = await tree.GetRootAsync();
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

static async Task<(SemanticModel Model, SyntaxTree Tree)> ProjectSemanticModel(string source, string filePath, string projectPath)
{
    if (!MSBuildLocator.IsRegistered)
        MSBuildLocator.RegisterDefaults();
    using var workspace = MSBuildWorkspace.Create();
    var project = await workspace.OpenProjectAsync(projectPath);
    if (workspace.Diagnostics.Any(d => d.Kind == WorkspaceDiagnosticKind.Failure))
    {
        foreach (var diag in workspace.Diagnostics.Where(d => d.Kind == WorkspaceDiagnosticKind.Failure))
            Console.Error.WriteLine($"MSBuild workspace warning: {diag.Message}");
    }
    var compilation = await project.GetCompilationAsync();
    if (compilation is null)
        Console.Error.WriteLine($"MSBuild workspace: no compilation produced for {projectPath}, falling back to platform-only analysis");
    // Determine the parse options used by the project so our tree has a matching language version.
    var projectParseOptions = (compilation?.SyntaxTrees.FirstOrDefault()?.Options as CSharpParseOptions)
        ?? new CSharpParseOptions();
    // Parse our source with project-aligned options so all trees in the compilation share the same version.
    var tree = CSharpSyntaxTree.ParseText(source, projectParseOptions, path: filePath);
    if (compilation is null)
    {
        return (PlatformSemanticModel(tree), tree);
    }
    // Build a new compilation that shares the project's references and all other source files
    // but substitutes our freshly-parsed tree for the target file. This ensures:
    // (a) the semantic model can resolve project-local types from sibling files, and
    // (b) our tree is part of the compilation and GetSemanticModel succeeds.
    var normalizedFile = Path.GetFullPath(filePath);
    var otherTrees = compilation.SyntaxTrees
        .Where(t => !string.Equals(
            Path.GetFullPath(t.FilePath ?? ""), normalizedFile, StringComparison.OrdinalIgnoreCase))
        .ToList();
    var projectCompilation = CSharpCompilation.Create(
        compilation.AssemblyName ?? "CalmRoslynAnalysis",
        otherTrees.Append(tree),
        compilation.References,
        (CSharpCompilationOptions)compilation.Options);
    return (projectCompilation.GetSemanticModel(tree), tree);
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

static async Task<int> RepositoryGraph(string repository)
{
    repository = Path.GetFullPath(repository);
    if (!Directory.Exists(repository)) { Console.Error.WriteLine($"repository not found: {repository}"); return 1; }
    if (!MSBuildLocator.IsRegistered) MSBuildLocator.RegisterDefaults();
    using var workspace = MSBuildWorkspace.Create();
    var solution = Directory.EnumerateFiles(repository, "*.sln", SearchOption.TopDirectoryOnly).OrderBy(x => x, StringComparer.Ordinal).FirstOrDefault();
    var projects = new List<Project>();
    var diagnostics = new List<GraphDiagnostic>();
    if (solution is not null)
    {
        projects.AddRange((await workspace.OpenSolutionAsync(solution)).Projects);
        diagnostics.AddRange(workspace.Diagnostics.Where(d => d.Kind == WorkspaceDiagnosticKind.Failure).Select(d => new GraphDiagnostic { Severity="error", Code="project-load", Message=d.Message }));
    }
    else
    {
        foreach (var csproj in Directory.EnumerateFiles(repository, "*.csproj", SearchOption.AllDirectories).Where(IsSourcePath).OrderBy(x => x, StringComparer.Ordinal))
        {
            try { await workspace.OpenProjectAsync(csproj); }
            catch (Exception ex) { diagnostics.Add(new GraphDiagnostic { Severity="error", Code="project-load", Project=csproj, Message=ex.Message }); Console.Error.WriteLine($"Roslyn project warning: {Path.GetFileName(csproj)}: {ex.Message}"); }
        }
        projects.AddRange(workspace.CurrentSolution.Projects);
    }
    var nodes = new Dictionary<string, GraphNode>(StringComparer.Ordinal);
    var edges = new Dictionary<(string Source,string Destination,string Kind), GraphEdge>();
    foreach (var project in projects.OrderBy(p => p.FilePath, StringComparer.Ordinal))
    {
        var compilation = await project.GetCompilationAsync(); if (compilation is null) { diagnostics.Add(new GraphDiagnostic { Severity="error", Code="compilation", Project=project.FilePath ?? project.Name, Message="no compilation produced" }); continue; }
        foreach (var tree in compilation.SyntaxTrees.OrderBy(t => t.FilePath, StringComparer.Ordinal))
        {
            if (!IsSourcePath(tree.FilePath)) continue;
            var model = compilation.GetSemanticModel(tree); var root = await tree.GetRootAsync();
            foreach (var type in root.DescendantNodes().OfType<BaseTypeDeclarationSyntax>())
            {
                var symbol = model.GetDeclaredSymbol(type) as INamedTypeSymbol; if (symbol is null || !IsInRepository(symbol, repository)) continue;
                var id = StableId(symbol); var span=type.GetLocation().GetLineSpan(); var node=new GraphNode { Id=id, Name=symbol.ToDisplayString(SymbolDisplayFormat.MinimallyQualifiedFormat), Kind=Kind(symbol,type), Project=project.Name, CanonicalName=CanonicalName(symbol) }; node.Locations.Add(new GraphLocation { File=Relative(repository,tree.FilePath), Line=span.StartLinePosition.Line+1, Column=span.StartLinePosition.Character+1 }); if (nodes.TryGetValue(id,out var existing)) existing.Locations.AddRange(node.Locations); else nodes.Add(id,node);
            }
            foreach (var syntax in root.DescendantNodes().Where(n => n is IdentifierNameSyntax or ObjectCreationExpressionSyntax or BaseTypeSyntax))
            {
                var target = model.GetSymbolInfo(syntax).Symbol ?? model.GetTypeInfo(syntax).Type; var source = ContainingType(model, syntax); if (target is null || source is null) continue;
                var destination = target as INamedTypeSymbol ?? target.ContainingType; if (!IsInRepository(source, repository) || !nodes.ContainsKey(StableId(source)) || destination is null || !IsInRepository(destination, repository) || SymbolEqualityComparer.Default.Equals(source,destination)) continue;
                var key=(StableId(source),StableId(destination),DependencyKind(syntax)); if (!edges.TryGetValue(key,out var edge)){edge=new GraphEdge{Source=key.Item1,Destination=key.Item2,Kind=key.Item3};edges[key]=edge;}
                var span=syntax.GetLocation().GetLineSpan(); edge.Locations.Add(new GraphLocation{File=Path.GetRelativePath(repository,tree.FilePath),Line=span.StartLinePosition.Line+1,Column=span.StartLinePosition.Character+1});
            }
        }
    }
    var result = new RepositoryGraph { SchemaVersion="1", Language="csharp", Nodes=nodes.Values.OrderBy(n=>n.Id,StringComparer.Ordinal).ToList(), Edges=edges.Values.OrderBy(e=>e.Source,StringComparer.Ordinal).ThenBy(e=>e.Destination,StringComparer.Ordinal).ThenBy(e=>e.Kind,StringComparer.Ordinal).ToList(), Analysis=new GraphAnalysis { Completeness=diagnostics.Count==0 ? "complete" : "incomplete", AnalyzedProjects=projects.Select(p=>p.Name).Distinct(StringComparer.Ordinal).OrderBy(x=>x,StringComparer.Ordinal).ToList(), SkippedProjects=new List<string>(), TargetFrameworks=new List<string>(), Diagnostics=diagnostics } };
    foreach (var edge in result.Edges) edge.Locations=edge.Locations.OrderBy(l=>l.File,StringComparer.Ordinal).ThenBy(l=>l.Line).ThenBy(l=>l.Column).GroupBy(l=>$"{l.File}:{l.Line}:{l.Column}").Select(g=>g.First()).ToList();
    Console.WriteLine(JsonSerializer.Serialize(result,new JsonSerializerOptions{PropertyNamingPolicy=JsonNamingPolicy.SnakeCaseLower,WriteIndented=true})); return 0;
}
static bool IsSourcePath(string? path) { if (string.IsNullOrEmpty(path)) return false; var full=Path.GetFullPath(path); if (full.Split(Path.DirectorySeparatorChar).Any(p=>p is "bin" or "obj" or ".git" or ".vs" or "packages" or ".tmp")) return false; return full.EndsWith(".cs",StringComparison.OrdinalIgnoreCase) || full.EndsWith(".csproj",StringComparison.OrdinalIgnoreCase); }
static string Relative(string repository,string? path) => string.IsNullOrEmpty(path) ? "" : Path.GetRelativePath(repository,path).Replace(Path.DirectorySeparatorChar,'/');
static bool IsInRepository(ISymbol symbol,string repo) { var path=symbol.Locations.FirstOrDefault(l=>l.IsInSource)?.SourceTree?.FilePath; return path is not null && Path.GetFullPath(path).StartsWith(repo+Path.DirectorySeparatorChar,StringComparison.OrdinalIgnoreCase) && IsSourcePath(path); }
static INamedTypeSymbol? ContainingType(SemanticModel model, SyntaxNode node)
{
    var declaration = node.AncestorsAndSelf().FirstOrDefault(n => n is TypeDeclarationSyntax or EnumDeclarationSyntax or DelegateDeclarationSyntax);
    var declared = declaration switch
    {
        TypeDeclarationSyntax type => model.GetDeclaredSymbol(type),
        EnumDeclarationSyntax enumeration => model.GetDeclaredSymbol(enumeration),
        DelegateDeclarationSyntax del => model.GetDeclaredSymbol(del),
        _ => null,
    };
    return declared ?? model.GetEnclosingSymbol(node.SpanStart)?.ContainingType;
}
static string Kind(INamedTypeSymbol symbol, BaseTypeDeclarationSyntax syntax) => syntax switch { RecordDeclarationSyntax r when r.ClassOrStructKeyword.IsKind(SyntaxKind.StructKeyword)=>"record-struct", RecordDeclarationSyntax=>"record-class", _=>symbol.TypeKind.ToString().ToLowerInvariant() switch { "class"=>"class", "interface"=>"interface", "struct"=>"struct", "enum"=>"enum", "delegate"=>"delegate", _=>"type" } };
static string CanonicalName(ISymbol symbol) => symbol.ToDisplayString(SymbolDisplayFormat.FullyQualifiedFormat).Replace("global::","");
static string StableId(ISymbol symbol) { var text=(symbol.ContainingAssembly?.Name??"")+"\0"+CanonicalName(symbol); return "csharp-"+Convert.ToHexString(System.Security.Cryptography.SHA256.HashData(System.Text.Encoding.UTF8.GetBytes(text))).ToLowerInvariant()[..24]; }
static string DependencyKind(SyntaxNode node) => node switch { ObjectCreationExpressionSyntax=>"constructs", BaseTypeSyntax=>"inherits", IdentifierNameSyntax id when id.Parent is ParameterSyntax=>"parameter", _=>"field" };

sealed class RepositoryGraph { public string SchemaVersion { get; set; } = "1"; public string Language { get; set; } = "csharp"; public List<GraphNode> Nodes { get; set; } = []; public List<GraphEdge> Edges { get; set; } = []; public GraphAnalysis Analysis { get; set; } = new(); }
sealed class GraphAnalysis { public string Completeness { get; set; } = "complete"; public List<string> AnalyzedProjects { get; set; } = []; public List<string> SkippedProjects { get; set; } = []; public List<string> TargetFrameworks { get; set; } = []; public List<GraphDiagnostic> Diagnostics { get; set; } = []; }
sealed class GraphDiagnostic { public string Severity { get; set; } = "error"; public string Code { get; set; } = ""; public string Message { get; set; } = ""; public string Project { get; set; } = ""; }
sealed class GraphNode { public string Id { get; set; } = ""; public string Name { get; set; } = ""; public string Kind { get; set; } = ""; public string Project { get; set; } = ""; public string CanonicalName { get; set; } = ""; public List<GraphLocation> Locations { get; set; } = []; }
sealed class GraphEdge { public string Source { get; set; } = ""; public string Destination { get; set; } = ""; public string Kind { get; set; } = ""; public List<GraphLocation> Locations { get; set; } = []; }
sealed class GraphLocation { public string File { get; set; } = ""; public int Line { get; set; } public int Column { get; set; } }
sealed record ImportEntry(string DisplayName, string NamespaceOrType, string Alias);
sealed class AnalysisResult { [JsonPropertyName("calm_node")] public string CALMNode { get; set; } = ""; public string Language { get; set; } = ""; public string File { get; set; } = ""; public List<FunctionMetric> Functions { get; set; } = []; [JsonPropertyName("file_metrics")] public FileMetric FileMetric { get; set; } = new(); [JsonPropertyName("module_metrics")] public ModuleMetric ModuleMetric { get; set; } = new(); [JsonPropertyName("import_metrics")] public ImportMetric Imports { get; set; } = new(); }
sealed class FunctionMetric { public string Name { get; set; } = ""; public int CyclomaticComplexity { get; set; } public bool IsPublic { get; set; } public int LOC { get; set; } }
sealed class FileMetric { public int TotalLOC { get; set; } public int LogicLOC { get; set; } public int PublicMethods { get; set; } public double LDR { get; set; } }
sealed class ModuleMetric { [JsonPropertyName("public_method_count")] public int PublicMethods { get; set; } [JsonPropertyName("total_loc")] public int TotalLOC { get; set; } [JsonPropertyName("private_loc")] public int PrivateLOC { get; set; } [JsonPropertyName("avg_loc_per_public_method")] public double AverageLOCPerPublicMethod { get; set; } }
sealed class ImportMetric { public int Total { get; set; } public int Used { get; set; } public List<string> Unused { get; set; } = []; public double DDC { get; set; } }
