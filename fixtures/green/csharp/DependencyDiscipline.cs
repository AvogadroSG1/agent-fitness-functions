using System;
using System.Collections.Generic;
using System.Globalization;
using System.IO;
using System.Linq;
using System.Net.Http;
using System.Text;
using System.Text.Json;
using System.Threading.Tasks;

namespace Demo;

public class ImportUse
{
    public async Task<string> DigestAsync(Dictionary<string, object> payload)
    {
        var json = JsonSerializer.Serialize(payload);
        var bytes = Encoding.UTF8.GetBytes(json);
        var path = Path.Combine("tmp", CultureInfo.InvariantCulture.Name);
        await Task.Yield();
        Console.WriteLine(HttpMethod.Get.Method);
        return string.Join(":", bytes.Length, path, payload.Keys.Count());
    }
}
