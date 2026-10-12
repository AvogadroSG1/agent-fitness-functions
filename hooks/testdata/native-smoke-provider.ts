type SmokeExtensionAPI = {
	registerProvider(name: string, config: {
		api: string;
		baseUrl: string;
		apiKey: string;
		models: Array<{
			id: string;
			name: string;
			reasoning: boolean;
			input: string[];
			cost: { input: number; output: number; cacheRead: number; cacheWrite: number };
			contextWindow: number;
			maxTokens: number;
		}>;
	}): void;
};

export default function (pi: SmokeExtensionAPI): void {
	const baseUrl = process.env.NATIVE_SMOKE_PROVIDER_URL;
	if (!baseUrl) throw new Error("NATIVE_SMOKE_PROVIDER_URL is required");
	pi.registerProvider("fitness-smoke", {
		api: "openai-completions",
		baseUrl,
		apiKey: "fixture",
		models: [
			{
				id: "fixture",
				name: "fixture",
				reasoning: false,
				input: ["text"],
				cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
				contextWindow: 8192,
				maxTokens: 1024,
			},
		],
	});
}
