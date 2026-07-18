import React from "react";
import { LS_VIEW, type ViewKey } from "./config";
import { Icon } from "./icons";
import { LANGS, detectLang, makeT, saveLang, type Lang } from "./i18n";
import { clearSddCache } from "./sdd-api";
import { ensureThemeInjected } from "./theme";
import { VIEWS } from "./views";

/**
 * "SDD Fleet" — the plugin's single exposed component (ADR-038). Registered at
 * `view` + `project.page`, reached from the "SDD Fleet" project nav item. It
 * renders a NATIVE left sub-rail with the eight fleet views and does its own
 * client-side sub-routing (React state, remembered in localStorage) — no host
 * router nesting, no iframe. Each view fetches SAME-ORIGIN /sdd-api/* (shared
 * 60 s cache) and renders native cards/tables/timelines.
 *
 * Class component + classic JSX on purpose (see tsconfig.json): the host share
 * scope provides the "react" specifier but not "react/jsx-runtime", and class
 * components survive a federation fallback to the bundled React copy where
 * hooks would crash.
 *
 * The host forwards different prop bags per surface (project.page passes only
 * {projectId}); SDD telemetry is team-wide, so we ignore projectId.
 */

// View key -> i18n title/subtitle prefix ("coordination" uses "coord.*").
const TITLE_PREFIX: Record<ViewKey, string> = {
	overview: "overview",
	tasks: "tasks",
	sessions: "sessions",
	activity: "activity",
	analytics: "analytics",
	coordination: "coord",
	sdd: "sdd",
	fleet: "fleet",
};

interface SddProps {
	projectId?: string;
	/** TEST-ONLY (smoke.mjs): initial view + language + seeded data, never set by the host. */
	__view?: ViewKey;
	__lang?: Lang;
	__testData?: unknown;
	[key: string]: unknown;
}

interface SddState {
	view: ViewKey;
	lang: Lang;
	refreshNonce: number;
}

function readInitialView(preferred?: ViewKey): ViewKey {
	if (preferred && VIEWS.some((v) => v.key === preferred)) return preferred;
	if (typeof localStorage !== "undefined") {
		const saved = localStorage.getItem(LS_VIEW);
		if (saved && VIEWS.some((v) => v.key === saved)) return saved as ViewKey;
	}
	return "overview";
}

export default class SddFleetView extends React.Component<SddProps, SddState> {
	constructor(props: SddProps) {
		super(props);
		ensureThemeInjected();
		this.state = {
			view: readInitialView(props.__view),
			lang: props.__lang ?? detectLang(),
			refreshNonce: 0,
		};
	}

	private setView = (view: ViewKey) => {
		this.setState({ view });
		if (typeof localStorage !== "undefined") localStorage.setItem(LS_VIEW, view);
	};

	private setLang = (lang: Lang) => {
		this.setState({ lang });
		saveLang(lang);
	};

	private refresh = () => {
		clearSddCache();
		this.setState((s) => ({ refreshNonce: s.refreshNonce + 1 }));
	};

	render() {
		const t = makeT(this.state.lang);
		const active = VIEWS.find((v) => v.key === this.state.view) ?? VIEWS[0];
		const prefix = TITLE_PREFIX[active.key];
		const Active = active.Component;

		return (
			<div className="gxsd-root">
				<div className="gxsd-shell">
					{/* Left sub-rail — the eight fleet views */}
					<nav className="gxsd-rail" aria-label={t("app.title")}>
						{VIEWS.map((v) => (
							<button
								type="button"
								key={v.key}
								className={`gxsd-rail-item ${v.key === active.key ? "active" : ""}`}
								onClick={() => this.setView(v.key)}
								aria-current={v.key === active.key ? "page" : undefined}
							>
								<span className="gxsd-rail-ico">
									<Icon name={v.icon} size={15} />
								</span>
								{t(`nav.${v.key}`)}
							</button>
						))}
					</nav>

					<div className="gxsd-main">
						<header className="gxsd-head">
							<div>
								<h2 className="gxsd-title">
									<span className="gxsd-rail-ico">
										<Icon name={active.icon} size={17} />
									</span>
									{t(`${prefix}.title`)}
								</h2>
								<p className="gxsd-sub">{t(`${prefix}.sub`)}</p>
							</div>
							<span className="gxsd-spacer" />
							<div className="gxsd-langs" role="group" aria-label="language">
								{LANGS.map((l) => (
									<button
										type="button"
										key={l.code}
										className={`gxsd-lang ${l.code === this.state.lang ? "active" : ""}`}
										onClick={() => this.setLang(l.code)}
									>
										{l.label}
									</button>
								))}
							</div>
							<button type="button" className="gxsd-btn" onClick={this.refresh}>
								<Icon name="refresh" size={13} />
								{t("act.refresh")}
							</button>
						</header>

						<div className="gxsd-body">
							{/* key=view remounts on switch: a fresh (cached) fetch, clean state */}
							<Active
								key={active.key}
								t={t}
								refreshNonce={this.state.refreshNonce}
								__testData={active.key === this.state.view ? this.props.__testData : undefined}
							/>
						</div>
					</div>
				</div>
			</div>
		);
	}
}
