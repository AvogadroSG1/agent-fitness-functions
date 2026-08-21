---
name: csharp
description: C# and .NET development guidelines. Use when writing C# code, creating .NET projects, reviewing C# PRs, or asking about C# best practices, patterns, testing, and project structure.
---

# C# / .NET Guidelines

These guidelines apply to all C# and .NET development.

## Language Features

### Types and Data
- Use records over classes for immutable data models
- Use `required` properties over constructor injection for DTOs/records
- Use nullable reference types (`<Nullable>enable</Nullable>`)
- Prefer `init` setters for immutable properties
- Use pattern matching where it improves readability

### Visibility and Encapsulation
- Use the lowest visibility modifiers possible
  - Prefer: `private` > `internal` > `protected internal` > `protected` > `public`
- Use `file`-scoped types for implementation details
- Use file-scoped namespaces (`namespace Foo;` not `namespace Foo { }`)

### Async/Await
- **MUST** use `async`/`await` for I/O-bound operations
- **MUST NOT** use `.Result` or `.Wait()` on tasks (causes deadlocks)
- Use `ConfigureAwait(false)` in library code
- Suffix async methods with `Async`

## Project Structure

### Solution Organization
- Organize code into small, focused projects that separate concerns
- **MUST NOT** create circular dependencies between projects
- **MUST NOT** create "god" projects with catch-all shared utilities
- **MUST NOT** Use `#region` to make code portions easier to filter when viewing
- Use the ports and adapters (hexagonal) patterns:
  - Within an application define:
    - `Models/` containing the domain models
    - `Ports/` contains interfaces describing the port
    - `Adapters/` contains a folder per port with the implementations of that port
    - Models required for talking to an API or a database go under the specific adapter folder
  - For API projects follow REST patterns
- Applications go under `src/`, tests under `tests/`

### Naming Conventions
| Element | Pattern | Example |
|---------|---------|---------|
| Projects | `StackOverflow.{Domain}.{Name of application}` | `StackOverflow.KnowledgeIngestion.IngestionConnector`, `StackOverflow.Content.WebApi` |
| Ports | Role based, `I{Role}` | `IRetrieveInventory`, `IStoreDocument` |
| Adapters | Technology based, `{Role}In{Technology}` | `StoreDocumentsInBlobStorage`, `RetrieveInventoryFromPostgres` |
| Async methods | `{Name}Async` | `GetUserAsync` |
| Test projects | `{ProjectName}.Tests.{Test type}` | `MyApp.Domain.Tests.Unit`, `MyApp.Domain.Tests.Integration`, `MyApp.Domain.Tests.EndToEnd` |

### Identifiers
- Use UUIDv7 as the default identifier strategy
- Use `Guid.CreateVersion7()` (native .NET 9+)
- Do not use auto-increment integers for distributed systems

## Testing

### Framework and Style
- Prefer **xUnit** as the testing framework
- Use **FluentAssertions** for readable assertions
- Prefer stubs over mocks.
- Use **NSubstitute** for mocking
- Follow Arrange-Act-Assert (AAA) pattern
- Use BDD patterns

### Test Organization
- Test class names: `When{Action}.cs`
- Test method names: `Given{Scenario}_{ExpectedResult}`
- Prefer setup methods over inlining
- Prefer builder pattern for test objects

```csharp
public sealed class WhenParsingJson
{
    [Test]
    public void GivenValidJson_ReturnsEvidenceLog()
    {
        var json = GivenValidJson();

        var result = WhenParsing(json);

        result.Should().NotBeNull();
    }

    private string GivenValidJson() => "{ ... }";

    private object WhenParsing(string json) => parser.Parse(json);
}
```

### Coverage
- **MUST** have tests for all business logic
- Should have integration tests for external boundaries
- May skip tests for trivial code (simple mappings, pass-through)

## Dependencies

### Package Management
- Use Central Package Management (`Directory.Packages.props`)
- **MUST** pin package versions explicitly

### Recommended Packages

| Purpose | Package |
|---------|---------|
| CLI | Spectre.Console |
| JSON | System.Text.Json (prefer over Newtonsoft) |
| YAML | YamlDotNet |
| Logging | Microsoft.Extensions.Logging |
| DI | Microsoft.Extensions.DependencyInjection |
| Result Pattern | ErrorOr |
| OpenTelemetry | OpenTelemetry.Extensions.Hosting |
| Testing | xUnit, FluentAssertions, NSubstitute |
| Validation | FluentValidation |

## Configuration

### Settings Pattern
- Use `IOptions<T>` pattern for configuration
- Use strongly-typed configuration classes
- **MUST NOT** hardcode configuration values

```csharp
public sealed record AppConfig
{
    public const string Section = nameof(AppConfig); // Use for configuration.GetSection() to obtain these values

    public required string Name { get; init; }
    public required string ConnectionString { get; init; }
    public required IReadOnlyList<FeatureConfig> Features { get; init; }
}
```

## Dependency Injection

- Use dependency injection for all services
- Use `Microsoft.Extensions.DependencyInjection` as the DI container
- Register dependencies in a separate class `DependencyRegistration.cs`
- Use constructor injection, avoid property injection
- Use appropriate lifetimes: `Singleton`, `Scoped`, `Transient`

## Error Handling

- Use the Result pattern over exceptions for expected failures
- Use **ErrorOr** package for Result pattern implementation
- **MUST** use exceptions for unexpected/exceptional conditions
- Create domain-specific exception types when needed
- **MUST NOT** swallow exceptions silently

### Result Pattern Example

```csharp
using ErrorOr;

public ErrorOr<User> GetUserAsync(Guid id)
{
    if (id == Guid.Empty)
        return Error.Validation("InvalidId", "User ID cannot be empty");

    var user = await _repository.FindAsync(id);
    if (user is null)
        return Error.NotFound("UserNotFound", $"User {id} not found");

    return user;
}

// Usage with Match
var result = await _service.GetUserAsync(id);
return result.Match(
    success => Ok(success),
    errors => HandleErrors(errors));

// Usage with IsError
if (result.IsError)
{
    _logger.LogWarning("Operation failed: {Errors}", result.Errors);
    return;
}
var user = result.Value;
```

## Code Style

### General
- Use expression-bodied members for simple operations
- Use target-typed `new()` when type is obvious
- Prefer `var` when type is obvious from context
- **MUST NOT** use `var` when type is not obvious

### Example Record

```csharp
namespace MyApp.Domain.Models;

public sealed record User(Guid Id, string Email, string DisplayName, UserRole Role, DateTimeOffset CreatedAt, Uri? AvatarUrl);

public enum UserRole { Member, Admin, Owner }
```

### Example port, adapter and use case

Port:
```csharp
namespace MyApp.Application.Ports;

public interface ICreateUser
{
    public Task<User> CreateUserAsync(CreateUserRequest request, CancellationToken cancellationToken);
}
```

Adapter:
```csharp
namespace MyApp.Application.Adapters;

internal sealed class CreateUserInPostgres(NpgsqlConnection connection) : ICreateUser
{
    public async Task<User> CreateUserAsync(CreateUserRequest request, CancellationToken cancellationToken)
    {
        const string insertQuery = "INSERT INTO users(name, avatarUrl) VALUES(@name, @avatarUrl) RETURNING *"
        return await connection.QuerySingleAsync<User>(insertQuery, new { name = request.Name, avatarUrl = request.AvatarUrl });
    }
}
```

Use case:
```csharp
namespace MyApp.Applications.UseCases;

internal sealed class CreateUserUseCase(ICreateUser createUser)
{
    public async Task<ErrorOr<User>> ExecuteAsync(CreateUserRequest request, CancellationToken cancellationToken)
    {
        if(!request.IsValid)
        {
            Error.Validation("request", "reason why request is invalid");
        }

        return await createUser.CreateUserAsync(request, cancellationToken);
    }
}
```
