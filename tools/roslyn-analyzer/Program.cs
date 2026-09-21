using System.Text.Json;
using System.Text.Json.Serialization;
using System.Xml;
using System.Xml.Linq;
using Microsoft.Build.Locator;
using Microsoft.CodeAnalysis;
using Microsoft.CodeAnalysis.CSharp;
using Microsoft.CodeAnalysis.CSharp.Syntax;
using Microsoft.CodeAnalysis.MSBuild;

// Args: <file.cs> [--project <foo.csproj>]
string? projectPath = null;
string? file = null;
string? solutionPath = null;
for (var i = 0; i < args.Length; i++)
{
    if (args[i] == "--project" && i + 1 < args.Length)
    {
        projectPath = args[++i];
    }
    else if (args[i] == "--solution" && i + 1 < args.Length)
    {
        solutionPath = args[++i];
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
	try { return await RepositoryGraph(args[repoIndex + 1], solutionPath); }
	catch (Exception ex) { Console.Error.WriteLine($"repository analysis failed: {ex.Message}"); return 1; }
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
    // Repository graph extraction should not require a full restore or turn
    // package-advisory metadata into a false semantic failure. The analyzer
    // only needs the compilations MSBuild can already construct locally.
    using var workspace = MSBuildWorkspace.Create(new Dictionary<string, string>
    {
        ["NuGetAudit"] = "false",
        ["RestoreIgnoreFailedSources"] = "true",
    });
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

static async Task<int> RepositoryGraph(string repository, string? requestedSolution)
{
    repository = Path.GetFullPath(repository);
	if (!Directory.Exists(repository)) { Console.Error.WriteLine($"repository not found: {repository}"); return 1; }
	var solution = ResolveSolution(repository, FindSolutions(repository), requestedSolution);
	if (IsUnsupportedSolution(solution)) return EmitProjectReferenceGraph(repository, solution, $"unsupported solution format: {Path.GetFileName(solution)} (.slnx requires compatible MSBuild tooling; pass --solution to a classic .sln)");
	if (!TryRegisterMSBuild(out var registrationError)) return EmitProjectReferenceGraph(repository, solution, registrationError);
	using var workspace = MSBuildWorkspace.Create();
	var loaded = await LoadProjects(workspace, repository, solution);
	if (loaded.Error is not null) return EmitProjectReferenceGraph(repository, solution, loaded.Error);
	var diagnostics = CollectWorkspaceDiagnostics(workspace, out var warnings);
	if (loaded.Projects.Count == 0 && loaded.Failed > 0) return EmitProjectReferenceGraph(repository, solution, $"no C# projects could be loaded semantically ({loaded.Failed} project load failures)");
	var graph = await BuildSemanticGraph(loaded.Projects, loaded.Failed, repository, diagnostics, warnings, solution);
    var result = graph.Result;
    var excludedCount = graph.Exclusions.Values.Sum(keys => keys.Count);
    result.Analysis.RelationshipCandidates = result.Edges.Count + excludedCount;
    result.Analysis.RelationshipsEmitted = result.Edges.Count;
	NormalizeEdges(result.Edges);
    Console.WriteLine(JsonSerializer.Serialize(result,new JsonSerializerOptions{PropertyNamingPolicy=JsonNamingPolicy.SnakeCaseLower,WriteIndented=true})); return 0;
}

static List<string> FindSolutions(string repository) => Directory.EnumerateFiles(repository, "*.*", SearchOption.TopDirectoryOnly)
	.Where(path => Path.GetExtension(path).Equals(".sln", StringComparison.OrdinalIgnoreCase) || Path.GetExtension(path).Equals(".slnx", StringComparison.OrdinalIgnoreCase))
	.OrderBy(x => x, StringComparer.Ordinal).ToList();

static bool IsUnsupportedSolution(string? solution) => solution is not null && Path.GetExtension(solution).Equals(".slnx", StringComparison.OrdinalIgnoreCase);

static bool TryRegisterMSBuild(out string error)
{
	try { if (!MSBuildLocator.IsRegistered) MSBuildLocator.RegisterDefaults(); error = ""; return true; }
	catch (Exception ex) { error = $"MSBuild toolchain unavailable: {ex.Message}"; return false; }
}

static async Task<(List<Project> Projects, int Failed, string? Error)> LoadProjects(MSBuildWorkspace workspace, string repository, string? solution)
{
	var projects = new List<Project>();
	if (solution is not null)
	{
		try { projects.AddRange((await workspace.OpenSolutionAsync(solution)).Projects.Where(project => Path.GetExtension(project.FilePath ?? "").Equals(".csproj", StringComparison.OrdinalIgnoreCase))); }
		catch (Exception ex) { return (projects, 0, $"semantic solution load failed: {ex.Message}"); }
		return (projects, 0, null);
	}
	var failed = 0;
	foreach (var csproj in Directory.EnumerateFiles(repository, "*.csproj", SearchOption.AllDirectories).Where(IsSourcePath).OrderBy(x => x, StringComparer.Ordinal))
	{
		try { projects.Add(await workspace.OpenProjectAsync(csproj)); }
		catch (Exception ex) { failed++; Console.Error.WriteLine($"Roslyn project warning: {Path.GetFileName(csproj)}: {ex.Message}"); }
	}
	return (projects, failed, null);
}

static List<string> CollectWorkspaceDiagnostics(MSBuildWorkspace workspace, out List<string> warnings)
{
	var diagnostics = new List<string>(); warnings = new List<string>();
	foreach (var diagnostic in workspace.Diagnostics.Where(d => d.Kind == WorkspaceDiagnosticKind.Failure))
	{
		if (IsNonBlockingWorkspaceDiagnostic(diagnostic.Message)) { warnings.Add(diagnostic.Message); Console.Error.WriteLine($"Roslyn workspace note: {diagnostic.Message}"); continue; }
		diagnostics.Add(diagnostic.Message); Console.Error.WriteLine($"Roslyn workspace warning: {diagnostic.Message}");
	}
	return diagnostics;
}

static async Task<(RepositoryGraph Result, Dictionary<string, HashSet<string>> Exclusions)> BuildSemanticGraph(List<Project> projects, int failedProjects, string repository, List<string> diagnostics, List<string> warnings, string? solution)
{
	var nodes = new Dictionary<string, GraphNode>(StringComparer.Ordinal);
	var edges = new Dictionary<(string Source,string Destination,string Kind), GraphEdge>();
	var exclusions = new Dictionary<string, HashSet<string>>(StringComparer.Ordinal);
	var projectIdentities = projects.Where(project => !string.IsNullOrWhiteSpace(project.FilePath)).GroupBy(project => project.AssemblyName ?? project.Name, StringComparer.Ordinal).ToDictionary(group => group.Key, group => Path.GetRelativePath(repository, group.First().FilePath!), StringComparer.Ordinal);
	foreach (var project in projects.OrderBy(p => p.FilePath, StringComparer.Ordinal)) await AddProjectGraph(project, repository, projectIdentities, nodes, edges, exclusions);
    var excludedCount = exclusions.Values.Sum(keys => keys.Count);
	return (new RepositoryGraph { Nodes=nodes.Values.OrderBy(n=>n.Id,StringComparer.Ordinal).ToList(), Edges=edges.Values.OrderBy(e=>e.Source,StringComparer.Ordinal).ThenBy(e=>e.Destination,StringComparer.Ordinal).ThenBy(e=>e.Kind,StringComparer.Ordinal).ToList(), Analysis = new AnalysisMetadata { AnalyzerVersion = "roslyn-repository-1", ExtractionMode = "semantic", Completeness = diagnostics.Count == 0 && failedProjects == 0 ? "complete-within-scope" : "partial", Diagnostics = diagnostics, Warnings = warnings, OmissionsByReason = exclusions.ToDictionary(item => item.Key, item => item.Value.Count, StringComparer.Ordinal), RelationshipsExcluded = excludedCount, Solutions = solution is null ? [] : [Path.GetRelativePath(repository, solution)], ProjectsDiscovered = projects.Count + failedProjects, ProjectsAnalyzed = projects.Count, ProjectsFailed = failedProjects } }, exclusions);
}

static async Task AddProjectGraph(Project project, string repository, Dictionary<string, string> projectIdentities, Dictionary<string, GraphNode> nodes, Dictionary<(string Source,string Destination,string Kind), GraphEdge> edges, Dictionary<string, HashSet<string>> exclusions)
{
	var compilation = await project.GetCompilationAsync();
	if (compilation is null) return;
	foreach (var tree in compilation.SyntaxTrees.OrderBy(t => t.FilePath, StringComparer.Ordinal))
		if (IsSourcePath(tree.FilePath)) await AddTreeGraph(compilation, tree, project, repository, projectIdentities, nodes, edges, exclusions);
}

static async Task AddTreeGraph(Compilation compilation, SyntaxTree tree, Project project, string repository, Dictionary<string, string> projectIdentities, Dictionary<string, GraphNode> nodes, Dictionary<(string Source,string Destination,string Kind), GraphEdge> edges, Dictionary<string, HashSet<string>> exclusions)
{
	var model = compilation.GetSemanticModel(tree); var root = await tree.GetRootAsync();
	foreach (var type in root.DescendantNodes().OfType<BaseTypeDeclarationSyntax>()) AddTypeNode(model, type, project, repository, projectIdentities, nodes);
	foreach (var syntax in root.DescendantNodes().Where(n => n is IdentifierNameSyntax or ObjectCreationExpressionSyntax or BaseTypeSyntax)) AddDependency(model, syntax, tree, repository, projectIdentities, edges, exclusions);
}

static void AddTypeNode(SemanticModel model, BaseTypeDeclarationSyntax type, Project project, string repository, Dictionary<string, string> projectIdentities, Dictionary<string, GraphNode> nodes)
{
	var symbol = model.GetDeclaredSymbol(type) as INamedTypeSymbol;
	if (symbol is null || !IsInRepository(symbol, repository)) return;
	var identity = ProjectIdentity(symbol, projectIdentities); var id = StableId(symbol, identity);
	nodes.TryAdd(id, BuildNode(symbol, id, project, repository, identity));
}

static void AddDependency(SemanticModel model, SyntaxNode syntax, SyntaxTree tree, string repository, Dictionary<string, string> projectIdentities, Dictionary<(string Source,string Destination,string Kind), GraphEdge> edges, Dictionary<string, HashSet<string>> exclusions)
{
	var target = model.GetSymbolInfo(syntax).Symbol ?? model.GetTypeInfo(syntax).Type; var source = ContainingType(model, syntax);
	if (target is null || source is null) return;
	var destination = target as INamedTypeSymbol ?? target.ContainingType;
	if (destination is null || !IsInRepository(destination, repository)) { RecordExclusion(exclusions, source, destination, target, syntax, repository, projectIdentities); return; }
	if (SymbolEqualityComparer.Default.Equals(source, destination)) return;
	var key = (StableId(source, ProjectIdentity(source, projectIdentities)), StableId(destination, ProjectIdentity(destination, projectIdentities)), DependencyKind(syntax, source, destination));
	if (!edges.TryGetValue(key, out var edge)) { edge = new GraphEdge { Source = key.Item1, Destination = key.Item2, Kind = key.Item3 }; edges[key] = edge; }
	var span = syntax.GetLocation().GetLineSpan(); var location = new GraphLocation { File = Path.GetRelativePath(repository, tree.FilePath), Line = span.StartLinePosition.Line + 1, Column = span.StartLinePosition.Character + 1 };
	edge.Locations.Add(location); edge.Evidence.Add(new GraphEvidence { Location = location, OriginatingMemberId = OriginatingMemberId(source, model.GetEnclosingSymbol(syntax.SpanStart), ProjectIdentity(source, projectIdentities)), ReferencedSymbol = target.ToDisplayString(SymbolDisplayFormat.FullyQualifiedFormat) });
}

static void RecordExclusion(Dictionary<string, HashSet<string>> exclusions, INamedTypeSymbol source, INamedTypeSymbol? destination, ISymbol target, SyntaxNode syntax, string repository, Dictionary<string, string> projectIdentities)
{
	var reason = ExclusionReason(destination, repository); var destinationID = destination is null ? target.ToDisplayString(SymbolDisplayFormat.FullyQualifiedFormat) : StableId(destination, ProjectIdentity(destination, projectIdentities));
	var key = StableId(source, ProjectIdentity(source, projectIdentities)) + "\u0000" + destinationID + "\u0000" + DependencyKind(syntax, source, destination ?? source);
	if (!exclusions.TryGetValue(reason, out var keys)) { keys = new HashSet<string>(StringComparer.Ordinal); exclusions[reason] = keys; }
	keys.Add(key);
}

static void NormalizeEdges(List<GraphEdge> edges)
{
	foreach (var edge in edges)
	{
		edge.Locations = edge.Locations.OrderBy(l=>l.File,StringComparer.Ordinal).ThenBy(l=>l.Line).ThenBy(l=>l.Column).GroupBy(l=>$"{l.File}:{l.Line}:{l.Column}").Select(g=>g.First()).ToList();
		edge.Evidence = edge.Evidence.OrderBy(e=>e.Location.File,StringComparer.Ordinal).ThenBy(e=>e.Location.Line).ThenBy(e=>e.Location.Column).GroupBy(e=>$"{e.Location.File}:{e.Location.Line}:{e.Location.Column}:{e.OriginatingMemberId}:{e.ReferencedSymbol}").Select(g=>g.First()).ToList();
	}
}

static int EmitProjectReferenceGraph(string repository, string? solution, string diagnostic)
{
    var projectPaths = Directory.EnumerateFiles(repository, "*.csproj", SearchOption.AllDirectories)
        .Where(IsSourcePath).OrderBy(path => path, StringComparer.Ordinal).ToList();
    var nodes = projectPaths.ToDictionary(path => path, path => {
        var relative = Path.GetRelativePath(repository, path).Replace(Path.DirectorySeparatorChar, '/');
        var id = "csharp-project-" + Convert.ToHexString(System.Security.Cryptography.SHA256.HashData(System.Text.Encoding.UTF8.GetBytes(relative))).ToLowerInvariant()[..16];
        var name = Path.GetFileNameWithoutExtension(path);
        return new GraphNode { Id = id, Name = name, QualifiedName = name, Kind = "project", Project = name, ProjectPath = relative, MemberExtractionAvailable = false, Metadata = new Dictionary<string, object?> { ["project_overview"] = true } };
    }, StringComparer.OrdinalIgnoreCase);
    var edges = new List<GraphEdge>();
    foreach (var projectPath in projectPaths)
    {
        XDocument document;
        try { document = XDocument.Load(projectPath, LoadOptions.SetLineInfo); }
        catch (Exception ex) { Console.Error.WriteLine($"project-reference warning: {Path.GetFileName(projectPath)}: {ex.Message}"); continue; }
        foreach (var reference in document.Descendants().Where(element => element.Name.LocalName == "ProjectReference"))
        {
            var include = (string?)reference.Attribute("Include");
            if (string.IsNullOrWhiteSpace(include)) continue;
            var destination = Path.GetFullPath(Path.Combine(Path.GetDirectoryName(projectPath)!, include));
            if (!nodes.ContainsKey(destination)) continue;
            var edge = new GraphEdge { Source = nodes[projectPath].Id, Destination = nodes[destination].Id, Kind = "project-reference" };
            if (reference is IXmlLineInfo lineInfo && lineInfo.HasLineInfo()) edge.Locations.Add(new GraphLocation { File = Path.GetRelativePath(repository, projectPath), Line = lineInfo.LineNumber, Column = lineInfo.LinePosition });
            edge.Evidence.Add(new GraphEvidence { Location = edge.Locations.FirstOrDefault() ?? new GraphLocation { File = Path.GetRelativePath(repository, projectPath) }, ReferencedSymbol = Path.GetRelativePath(repository, destination) });
            edges.Add(edge);
        }
    }
    var result = new RepositoryGraph {
        Nodes = nodes.Values.OrderBy(node => node.Id, StringComparer.Ordinal).ToList(),
        Edges = edges.GroupBy(edge => (edge.Source, edge.Destination, edge.Kind)).Select(group => group.First()).OrderBy(edge => edge.Source, StringComparer.Ordinal).ThenBy(edge => edge.Destination, StringComparer.Ordinal).ToList(),
        Analysis = new AnalysisMetadata {
            AnalyzerVersion = "roslyn-repository-1", ExtractionMode = "project-reference", Completeness = "partial",
            Diagnostics = [diagnostic], Solutions = solution is null ? [] : [Path.GetRelativePath(repository, solution)],
            ProjectsDiscovered = projectPaths.Count, ProjectsAnalyzed = projectPaths.Count, ProjectsFailed = 0,
            RelationshipCandidates = edges.Count, RelationshipsEmitted = edges.Count,
            OmissionsByReason = new Dictionary<string, int> { ["semantic-project-load"] = projectPaths.Count }
        }
    };
    Console.Error.WriteLine($"Roslyn semantic extraction unavailable; emitted {result.Nodes.Count} projects and {result.Edges.Count} project references in dependency-light mode.");
    Console.WriteLine(JsonSerializer.Serialize(result, new JsonSerializerOptions { PropertyNamingPolicy = JsonNamingPolicy.SnakeCaseLower, WriteIndented = true }));
    return 0;
}
static bool IsNonBlockingWorkspaceDiagnostic(string message)
{
    // NuGet audit findings are useful security notes, but they do not prevent
    // Roslyn from building a compilation when restore has otherwise succeeded.
    if (message.Contains("known ", StringComparison.OrdinalIgnoreCase) && message.Contains("severity vulnerability", StringComparison.OrdinalIgnoreCase)) return true;
    // Docker Compose projects are intentionally outside the C# semantic graph.
    if (message.Contains(".dcproj", StringComparison.OrdinalIgnoreCase) && message.Contains("not associated with a language", StringComparison.OrdinalIgnoreCase)) return true;
    return false;
}
static string? ResolveSolution(string repository, List<string> solutions, string? requested)
{
    if (!string.IsNullOrWhiteSpace(requested))
    {
        var candidate = Path.GetFullPath(Path.IsPathRooted(requested) ? requested : Path.Combine(repository, requested));
        if (!candidate.StartsWith(repository + Path.DirectorySeparatorChar, StringComparison.OrdinalIgnoreCase) || !File.Exists(candidate)) throw new InvalidOperationException($"solution must be an existing file inside the repository: {requested}");
        return candidate;
    }
    var classic = solutions.Where(path => Path.GetExtension(path).Equals(".sln", StringComparison.OrdinalIgnoreCase)).ToList();
    var preferred = classic.Where(path => !Path.GetFileNameWithoutExtension(path).Equals("Everything", StringComparison.OrdinalIgnoreCase)).ToList();
    if (preferred.Count == 1) return preferred[0];
    if (classic.Count == 1) return classic[0];
    if (classic.Count > 1) throw new InvalidOperationException($"multiple solutions found; pass --solution explicitly: {string.Join(", ", classic.Select(Path.GetFileName))}");
    return solutions.FirstOrDefault();
}
static bool IsSourcePath(string? path) { if (string.IsNullOrEmpty(path)) return false; var full=Path.GetFullPath(path); if (full.Split(Path.DirectorySeparatorChar).Any(p=>p is "bin" or "obj" or ".git" or ".vs" or "packages")) return false; return full.EndsWith(".cs",StringComparison.OrdinalIgnoreCase) || full.EndsWith(".csproj",StringComparison.OrdinalIgnoreCase); }
static string ExclusionReason(INamedTypeSymbol? symbol, string repository)
{
    if (symbol is null) return "unresolved-symbol";
    if (symbol.IsImplicitlyDeclared) return "generated-symbol";
    var location = symbol.Locations.FirstOrDefault(l => l.IsInSource)?.SourceTree?.FilePath;
    if (location is null) return "external-definition";
    if (!IsSourcePath(location)) return "generated-source";
    if (!Path.GetFullPath(location).StartsWith(repository + Path.DirectorySeparatorChar, StringComparison.OrdinalIgnoreCase)) return "external-definition";
    if (symbol.IsGenericType && !SymbolEqualityComparer.Default.Equals(symbol, symbol.OriginalDefinition)) return "generic-identity";
    return "identity-mismatch";
}
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
    return declared?.ContainingType ?? model.GetEnclosingSymbol(node.SpanStart)?.ContainingType;
}
static string Kind(INamedTypeSymbol symbol) => symbol.TypeKind.ToString().ToLowerInvariant() switch { "class"=>"class", "interface"=>"interface", "struct"=>"struct", "enum"=>"enum", "delegate"=>"delegate", _=>"type" };
static string ProjectIdentity(ISymbol symbol, IReadOnlyDictionary<string, string> projectIdentities)
{
    var assembly = symbol.ContainingAssembly?.Name ?? "";
    return projectIdentities.TryGetValue(assembly, out var identity) ? identity : assembly;
}
static string StableId(ISymbol symbol, string projectIdentity = "")
{
    // References to constructed generic types must resolve to the same node as
    // their declaration. OriginalDefinition removes type arguments while
    // retaining the fully-qualified, checkout-independent symbol identity.
    if (symbol is INamedTypeSymbol named && named.IsGenericType) symbol = named.OriginalDefinition;
    var identity = (string.IsNullOrWhiteSpace(projectIdentity) ? symbol.ContainingAssembly?.Name ?? "" : projectIdentity) + "/" + symbol.ToDisplayString(SymbolDisplayFormat.FullyQualifiedFormat);
    return "csharp-" + Convert.ToHexString(System.Security.Cryptography.SHA256.HashData(System.Text.Encoding.UTF8.GetBytes(identity))).ToLowerInvariant()[..16];
}
static string DependencyKind(SyntaxNode node, INamedTypeSymbol source, INamedTypeSymbol destination) => node switch { ObjectCreationExpressionSyntax=>"construction", BaseTypeSyntax when SymbolEqualityComparer.Default.Equals(source.BaseType, destination)=>"inheritance", BaseTypeSyntax=>"implements", IdentifierNameSyntax id when id.Parent is ParameterSyntax=>"parameter", _=>"member" };

static GraphNode BuildNode(INamedTypeSymbol symbol, string id, Project project, string repository, string projectIdentity)
{
    return new GraphNode {
        Id = id, Name = symbol.ToDisplayString(SymbolDisplayFormat.MinimallyQualifiedFormat), QualifiedName = symbol.ToDisplayString(SymbolDisplayFormat.FullyQualifiedFormat),
        Namespace = symbol.ContainingNamespace?.ToDisplayString() ?? "", Kind = Kind(symbol), Project = project.Name,
        ProjectPath = project.FilePath is null ? "" : Path.GetRelativePath(repository, project.FilePath), Accessibility = symbol.DeclaredAccessibility.ToString().ToLowerInvariant(),
        IsAbstract = symbol.IsAbstract, IsStatic = symbol.IsStatic, IsSealed = symbol.IsSealed,
        DeclarationLocations = symbol.Locations.Where(location => location.IsInSource).Select(location => LocationOf(location, repository)).ToList(),
        Members = ExtractMembers(symbol, repository, projectIdentity), MemberExtractionAvailable = true
    };
}

static List<GraphMember> ExtractMembers(INamedTypeSymbol symbol, string repository, string projectIdentity)
{
    return symbol.GetMembers().Where(member => !member.IsImplicitlyDeclared && member.Locations.Any(location => location.IsInSource)).Select(member => {
        var kind = member switch { IPropertySymbol => "property", IFieldSymbol => "field", IMethodSymbol m when m.MethodKind == MethodKind.Constructor => "constructor", IMethodSymbol m when m.MethodKind == MethodKind.Ordinary => "method", _ => "" };
        if (kind == "") return null;
        var property = member as IPropertySymbol;
        var method = member as IMethodSymbol;
        return new GraphMember {
            Id = MemberId(symbol, member, projectIdentity), Kind = kind, Name = member.Name, DisplaySignature = member.ToDisplayString(SymbolDisplayFormat.MinimallyQualifiedFormat),
            Accessibility = member.DeclaredAccessibility.ToString().ToLowerInvariant(), Type = property?.Type.ToDisplayString(SymbolDisplayFormat.MinimallyQualifiedFormat) ?? method?.ReturnType.ToDisplayString(SymbolDisplayFormat.MinimallyQualifiedFormat) ?? (member as IFieldSymbol)?.Type.ToDisplayString(SymbolDisplayFormat.MinimallyQualifiedFormat) ?? "",
            IsStatic = member.IsStatic, IsAbstract = member.IsAbstract, IsReadOnly = (member as IFieldSymbol)?.IsReadOnly ?? false,
            PropertyGetAccessibility = property?.GetMethod?.DeclaredAccessibility.ToString().ToLowerInvariant(), PropertySetAccessibility = property?.SetMethod?.DeclaredAccessibility.ToString().ToLowerInvariant(),
            Parameters = method?.Parameters.Select(parameter => new GraphParameter { Name = parameter.Name, Type = parameter.Type.ToDisplayString(SymbolDisplayFormat.MinimallyQualifiedFormat), RefKind = parameter.RefKind.ToString().ToLowerInvariant(), IsOptional = parameter.IsOptional }).ToList() ?? [],
            Locations = member.Locations.Where(location => location.IsInSource).Select(location => LocationOf(location, repository)).ToList()
        };
    }).Where(member => member is not null).Cast<GraphMember>().OrderBy(member => member.Id, StringComparer.Ordinal).ToList();
}

static string MemberId(INamedTypeSymbol type, ISymbol member, string projectIdentity = "")
{
    // FullyQualifiedFormat intentionally omits constructor parameter lists for
    // some Roslyn symbols, so overloaded constructors would otherwise collide.
    // Documentation IDs include the signature and remain checkout-independent.
    var identity = member.GetDocumentationCommentId() ?? member.ToDisplayString(SymbolDisplayFormat.FullyQualifiedFormat);
    return StableId(type, projectIdentity) + "-" + Convert.ToHexString(System.Security.Cryptography.SHA256.HashData(System.Text.Encoding.UTF8.GetBytes(identity))).ToLowerInvariant()[..16];
}
static string? OriginatingMemberId(INamedTypeSymbol type, ISymbol? member, string projectIdentity) => member is null || member.ContainingType is null || member is not (IMethodSymbol or IPropertySymbol or IFieldSymbol or IEventSymbol) ? null : MemberId(type, member, projectIdentity);
static GraphLocation LocationOf(Location location, string repository) { var span = location.GetLineSpan(); return new GraphLocation { File = Path.GetRelativePath(repository, span.Path), Line = span.StartLinePosition.Line + 1, Column = span.StartLinePosition.Character + 1 }; }

sealed class RepositoryGraph { [JsonInclude] internal List<GraphNode> Nodes { get; set; } = []; [JsonInclude] internal List<GraphEdge> Edges { get; set; } = []; [JsonInclude] internal AnalysisMetadata Analysis { get; set; } = new(); }
sealed class AnalysisMetadata { [JsonInclude] internal string ExtensionVersion { get; set; } = "1"; [JsonInclude] internal string IdentityVersion { get; set; } = "1"; [JsonInclude] internal string AnalyzerVersion { get; set; } = ""; [JsonInclude] internal string ExtractionMode { get; set; } = ""; [JsonInclude] internal string Completeness { get; set; } = ""; [JsonInclude] internal List<string> Diagnostics { get; set; } = []; [JsonInclude] internal List<string> Warnings { get; set; } = []; [JsonInclude] internal Dictionary<string,int> OmissionsByReason { get; set; } = new(); [JsonInclude] internal List<string> Solutions { get; set; } = []; [JsonInclude] internal int ProjectsDiscovered { get; set; } [JsonInclude] internal int ProjectsAnalyzed { get; set; } [JsonInclude] internal int ProjectsFailed { get; set; } [JsonInclude] internal int RelationshipCandidates { get; set; } [JsonInclude] internal int RelationshipsEmitted { get; set; } [JsonInclude] internal int RelationshipsExcluded { get; set; } }
sealed class GraphNode { [JsonInclude] internal string Id { get; set; } = ""; [JsonInclude] internal string Name { get; set; } = ""; [JsonInclude] internal string QualifiedName { get; set; } = ""; [JsonInclude] internal string Namespace { get; set; } = ""; [JsonInclude] internal string Kind { get; set; } = ""; [JsonInclude] internal string Project { get; set; } = ""; [JsonInclude] internal string ProjectPath { get; set; } = ""; [JsonInclude] internal string Accessibility { get; set; } = ""; [JsonInclude] internal bool IsAbstract { get; set; } [JsonInclude] internal bool IsStatic { get; set; } [JsonInclude] internal bool IsSealed { get; set; } [JsonInclude] internal bool MemberExtractionAvailable { get; set; } [JsonInclude] internal Dictionary<string, object?> Metadata { get; set; } = new(); [JsonInclude] internal List<GraphLocation> DeclarationLocations { get; set; } = []; [JsonInclude] internal List<GraphMember> Members { get; set; } = []; }
sealed class GraphMember { [JsonInclude] internal string Id { get; set; } = ""; [JsonInclude] internal string Kind { get; set; } = ""; [JsonInclude] internal string Name { get; set; } = ""; [JsonInclude] internal string DisplaySignature { get; set; } = ""; [JsonInclude] internal string Accessibility { get; set; } = ""; [JsonInclude] internal string Type { get; set; } = ""; [JsonInclude] internal bool IsStatic { get; set; } [JsonInclude] internal bool IsAbstract { get; set; } [JsonInclude] internal bool IsReadOnly { get; set; } [JsonInclude] internal string? PropertyGetAccessibility { get; set; } [JsonInclude] internal string? PropertySetAccessibility { get; set; } [JsonInclude] internal List<GraphParameter> Parameters { get; set; } = []; [JsonInclude] internal List<GraphLocation> Locations { get; set; } = []; }
sealed class GraphParameter { [JsonInclude] internal string Name { get; set; } = ""; [JsonInclude] internal string Type { get; set; } = ""; [JsonInclude] internal string RefKind { get; set; } = ""; [JsonInclude] internal bool IsOptional { get; set; } }
sealed class GraphEdge { [JsonInclude] internal string Source { get; set; } = ""; [JsonInclude] internal string Destination { get; set; } = ""; [JsonInclude] internal string Kind { get; set; } = ""; [JsonInclude] internal List<GraphLocation> Locations { get; set; } = []; [JsonInclude] internal List<GraphEvidence> Evidence { get; set; } = []; }
sealed class GraphEvidence { [JsonInclude] internal GraphLocation Location { get; set; } = new(); [JsonInclude] internal string? OriginatingMemberId { get; set; } [JsonInclude] internal string ReferencedSymbol { get; set; } = ""; }
sealed class GraphLocation { [JsonInclude] internal string File { get; set; } = ""; [JsonInclude] internal int Line { get; set; } [JsonInclude] internal int Column { get; set; } }
sealed record ImportEntry(string DisplayName, string NamespaceOrType, string Alias);
sealed class AnalysisResult { [JsonPropertyName("calm_node")] [JsonInclude] internal string CALMNode { get; set; } = ""; [JsonInclude] internal string Language { get; set; } = ""; [JsonInclude] internal string File { get; set; } = ""; [JsonInclude] internal List<FunctionMetric> Functions { get; set; } = []; [JsonPropertyName("file_metrics")] [JsonInclude] internal FileMetric FileMetric { get; set; } = new(); [JsonPropertyName("module_metrics")] [JsonInclude] internal ModuleMetric ModuleMetric { get; set; } = new(); [JsonPropertyName("import_metrics")] [JsonInclude] internal ImportMetric Imports { get; set; } = new(); }
sealed class FunctionMetric { [JsonInclude] internal string Name { get; set; } = ""; [JsonInclude] internal int CyclomaticComplexity { get; set; } [JsonInclude] internal bool IsPublic { get; set; } [JsonInclude] internal int LOC { get; set; } }
sealed class FileMetric { [JsonInclude] internal int TotalLOC { get; set; } [JsonInclude] internal int LogicLOC { get; set; } [JsonInclude] internal int PublicMethods { get; set; } [JsonInclude] internal double LDR { get; set; } }
sealed class ModuleMetric { [JsonPropertyName("public_method_count")] [JsonInclude] internal int PublicMethods { get; set; } [JsonPropertyName("total_loc")] [JsonInclude] internal int TotalLOC { get; set; } [JsonPropertyName("private_loc")] [JsonInclude] internal int PrivateLOC { get; set; } [JsonPropertyName("avg_loc_per_public_method")] [JsonInclude] internal double AverageLOCPerPublicMethod { get; set; } }
sealed class ImportMetric { [JsonInclude] internal int Total { get; set; } [JsonInclude] internal int Used { get; set; } [JsonInclude] internal List<string> Unused { get; set; } = []; [JsonInclude] internal double DDC { get; set; } }
