import "i18next";

import admin from "./locales/en/admin.json";
import appShell from "./locales/en/appShell.json";
import auth from "./locales/en/auth.json";
import common from "./locales/en/common.json";
import errors from "./locales/en/errors.json";
import plugins from "./locales/en/plugins.json";
import profile from "./locales/en/profile.json";
import projects from "./locales/en/projects.json";
import shared from "./locales/en/shared.json";
import shortcuts from "./locales/en/shortcuts.json";

declare module "i18next" {
	interface CustomTypeOptions {
		defaultNS: "common";
		resources: {
			admin: typeof admin;
			appShell: typeof appShell;
			auth: typeof auth;
			common: typeof common;
			errors: typeof errors;
			plugins: typeof plugins;
			profile: typeof profile;
			projects: typeof projects;
			shared: typeof shared;
			shortcuts: typeof shortcuts;
		};
	}
}
