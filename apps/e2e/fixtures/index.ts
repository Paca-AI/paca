import { test as base } from "@playwright/test";
import { LoginPage } from "../pages/login.page";

type Fixtures = {
	loginPage: LoginPage;
};

/**
 * Extended test fixture that provides a `loginPage` instance pre-navigated to `/`.
 *
 * Usage — import `test` and `expect` from this module instead of `@playwright/test`
 * in any test that interacts with the login page:
 *
 * ```ts
 * import { test, expect } from '../../fixtures';
 *
 * test('example', async ({ loginPage }) => {
 *   await loginPage.login('admin', 'secret');
 * });
 * ```
 */
export const test = base.extend<Fixtures>({
	loginPage: async ({ page }, use) => {
		const loginPage = new LoginPage(page);
		await loginPage.goto();
		await use(loginPage);
	},
});

export { expect } from "@playwright/test";
