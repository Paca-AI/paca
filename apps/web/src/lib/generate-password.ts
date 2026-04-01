export function generatePassword(): string {
	const upper = "ABCDEFGHIJKLMNOPQRSTUVWXYZ";
	const lower = "abcdefghijklmnopqrstuvwxyz";
	const digits = "0123456789";
	const symbols = "!@#$%^&*()-_=+[]{}";
	const all = upper + lower + digits + symbols;
	const arr = new Uint32Array(16);
	crypto.getRandomValues(arr);
	// Guarantee at least one of each required class in the first 4 positions
	const required = [
		upper[arr[0] % upper.length],
		lower[arr[1] % lower.length],
		digits[arr[2] % digits.length],
		symbols[arr[3] % symbols.length],
	];
	const rest = Array.from(arr.slice(4), (n) => all[n % all.length]);
	// Shuffle using Fisher-Yates with crypto values
	const combined = [...required, ...rest];
	const shuffle = new Uint32Array(combined.length);
	crypto.getRandomValues(shuffle);
	for (let i = combined.length - 1; i > 0; i--) {
		const j = shuffle[i] % (i + 1);
		[combined[i], combined[j]] = [combined[j], combined[i]];
	}
	return combined.join("");
}
