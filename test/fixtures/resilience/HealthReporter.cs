using System;
using System.Collections.Generic;
using System.Threading;
using System.Threading.Tasks;

namespace Observatory.Monitoring;

public interface IHealthReporter
{
    Task<HealthReport> CheckHealthAsync(CancellationToken cancellationToken = default);
    void RecordMetric(string name, double value);
}

public enum HealthStatus
{
    Healthy,
    Degraded,
    Unhealthy
}

public class HealthReport
{
    public HealthStatus Status { get; set; }
    public TimeSpan Duration { get; set; }
    public Dictionary<string, string> Entries { get; set; } = new();

    public HealthReport(HealthStatus status, TimeSpan duration)
    {
        Status = status;
        Duration = duration;
    }
}

public class HealthReporter : IHealthReporter
{
    private readonly Dictionary<string, double> _metrics = new();
    private readonly object _lock = new();

    public HealthReporter()
    {
    }

    public Task<HealthReport> CheckHealthAsync(CancellationToken cancellationToken = default)
    {
        if (cancellationToken.IsCancellationRequested)
        {
            return Task.FromCanceled<HealthReport>(cancellationToken);
        }

        var report = new HealthReport(HealthStatus.Healthy, TimeSpan.FromMilliseconds(5));
        lock (_lock)
        {
            foreach (var kvp in _metrics)
            {
                report.Entries[kvp.Key] = kvp.Value.ToString("F2");
            }
        }

        return Task.FromResult(report);
    }

    public void RecordMetric(string name, double value)
    {
        if (string.IsNullOrWhiteSpace(name))
        {
            throw new ArgumentException("Metric name cannot be null or empty", nameof(name));
        }

        lock (_lock)
        {
            _metrics[name] = value;
        }
    }
}
