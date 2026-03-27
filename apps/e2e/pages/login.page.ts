import { expect, type Locator, type Page } from "@playwright/test";

/**
 * Page Object Model for the login page.
 *
 * Encapsulates all selectors and interactions with the login page so that
 * individual test files stay concise and selector changes can be fixed
 * in a single place.
 */
export class LoginPage {
	readonly usernameInput: Locator;
	readonly passwordInput: Locator;
	readonly signInButton: Locator;
	readonly rememberMeSwitch: Locator;
	readonly errorMessage: Locator;
	readonly themeToggle: Locator;
	readonly showPasswordButton: Locator;
	readonly hidePasswordButton: Locator;

	constructor(readonly page: Page) {
		this.usernameInput = page.getByRole("textbox", { name: "Username" });
		this.passwordInput = page.getByRole("textbox", { name: "Password" });
		this.signInButton = page.getByRole("button", { name: "Sign in" });
		this.rememberMeSwitch = page.getByRole("switch", { name: "Keep me signed in" });
		this.errorMessage = page.getByText("Invalid username or password.");
		this.themeToggle = page.getByRole("button", { name: /Theme mode/i });
		this.showPasswordButton = page.getByRole("button", {
			name: "Show password",
		});
		this.hidePasswordButton = page.getByRole("button", {
			name: "Hide password",
		});
	}

	async goto() {
		await this.page.goto("/");
	}

	async fill(username: string, password: string) {
		await this.usernameInput.fill(username);
		await this.passwordInput.fill(password);
	}

	async submit() {
		await this.signInButton.click();
	}

	async login(username: string, password: string) {
		await this.fill(username, password);
		await this.submit();
	}

	async expectFormVisible() {
		await expect(this.usernameInput).toBeVisible();
		await expect(this.passwordInput).toBeVisible();
		await expect(this.signInButton).toBeVisible();
	}
}
