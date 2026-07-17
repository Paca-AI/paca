import React from "react";
import { LOAD_TIMEOUT_MS, PACA_ORIGIN, SDD_HOST, SDD_URL } from "./config";

// Class component + React.createElement only (no hooks): hooks require the
// dispatcher of the exact React copy that renders us. Shared resolution
// normally hands us the host's React 19 singleton, but if the federation
// share scope ever falls back to the bundled copy, class components keep
// working across copies (the reconciler only duck-checks
// prototype.isReactComponent and injects its own updater) while hooks would
// crash. Cheap insurance for a plugin that outlives host upgrades.

interface SddFrameState {
	/** "loading" until the iframe fires onload, "timeout" if it never does. */
	phase: "loading" | "loaded" | "timeout";
	/** Operator dismissed the fallback overlay ("keep waiting"). */
	dismissed: boolean;
	/** Bumped by Retry to force a full iframe remount. */
	attempt: number;
}

const wrapStyle: React.CSSProperties = {
	position: "relative",
	flex: "1 1 0%",
	minHeight: 0,
	width: "100%",
	height: "100%",
	display: "flex",
};

const frameStyle: React.CSSProperties = {
	border: 0,
	width: "100%",
	height: "100%",
	flex: "1 1 0%",
	background: "transparent",
};

const overlayStyle: React.CSSProperties = {
	position: "absolute",
	inset: 0,
	display: "flex",
	alignItems: "center",
	justifyContent: "center",
	padding: 24,
	background: "var(--background, rgba(20, 20, 24, 0.92))",
	zIndex: 10,
};

const panelStyle: React.CSSProperties = {
	maxWidth: 560,
	fontSize: 13,
	lineHeight: 1.55,
	color: "var(--foreground, #ddd)",
	border: "1px solid var(--border, rgba(128,128,128,0.35))",
	borderRadius: 10,
	padding: "18px 20px",
	background: "var(--card, transparent)",
};

const buttonStyle: React.CSSProperties = {
	font: "inherit",
	fontSize: 12,
	padding: "5px 12px",
	borderRadius: 6,
	border: "1px solid var(--border, rgba(128,128,128,0.45))",
	background: "transparent",
	color: "inherit",
	cursor: "pointer",
};

/**
 * The embedded SDD sensor dashboard.
 *
 * Fallback detection is the pragmatic onload-timer heuristic: if the iframe
 * has not fired `load` after LOAD_TIMEOUT_MS we surface an operator hint.
 * Cross-origin JS cannot reliably distinguish "blocked by X-Frame-Options /
 * frame-ancestors" from "slow network" — and Chrome even fires `load` for its
 * own XFO error page — so the overlay is advisory (dismissable, iframe stays
 * mounted) and an "Open in new tab" escape hatch is always rendered by the
 * surrounding chrome.
 */
export default class SddFrame extends React.Component<
	Record<string, never>,
	SddFrameState
> {
	private timer: ReturnType<typeof setTimeout> | undefined;

	state: SddFrameState = { phase: "loading", dismissed: false, attempt: 0 };

	componentDidMount() {
		this.armTimer();
	}

	componentWillUnmount() {
		if (this.timer !== undefined) clearTimeout(this.timer);
	}

	private armTimer() {
		if (this.timer !== undefined) clearTimeout(this.timer);
		this.timer = setTimeout(() => {
			this.setState((s) =>
				s.phase === "loading" ? { ...s, phase: "timeout" } : s,
			);
		}, LOAD_TIMEOUT_MS);
	}

	private handleLoad = () => {
		if (this.timer !== undefined) clearTimeout(this.timer);
		this.setState((s) => ({ ...s, phase: "loaded" }));
	};

	private handleRetry = () => {
		this.setState(
			(s) => ({
				phase: "loading",
				dismissed: false,
				attempt: s.attempt + 1,
			}),
			() => this.armTimer(),
		);
	};

	private handleDismiss = () => {
		this.setState((s) => ({ ...s, dismissed: true }));
	};

	render() {
		const { phase, dismissed, attempt } = this.state;
		const showFallback = phase === "timeout" && !dismissed;

		return (
			<div style={wrapStyle}>
				<iframe
					key={attempt}
					src={SDD_URL}
					title="SDD sensor dashboard"
					style={frameStyle}
					// The sensor is a same-origin-cookied SPA behind Vortex OIDC:
					// it needs its own origin (allow-same-origin) and scripts; forms
					// and popups cover the OIDC hop when no session exists yet.
					sandbox="allow-same-origin allow-scripts allow-forms allow-popups allow-popups-to-escape-sandbox allow-modals allow-downloads"
					allow="clipboard-read; clipboard-write; fullscreen"
					referrerPolicy="strict-origin-when-cross-origin"
					onLoad={this.handleLoad}
				/>
				{showFallback ? (
					<div style={overlayStyle}>
						<div style={panelStyle}>
							<div style={{ fontWeight: 600, marginBottom: 8 }}>
								SDD sensor dashboard did not load
							</div>
							<p style={{ margin: "0 0 8px" }}>
								The embed at <code>{SDD_HOST}</code> has not finished loading
								after {Math.round(LOAD_TIMEOUT_MS / 1000)}s. If the frame stays
								blank, the sensor is refusing to be embedded: on the sensor
								deployment ({SDD_HOST}), <strong>unset X-Frame-Options</strong>{" "}
								(or set <code>Content-Security-Policy: frame-ancestors</code>{" "}
								to allow <code>{PACA_ORIGIN}</code>).
							</p>
							<p style={{ margin: "0 0 12px", opacity: 0.8 }}>
								A missing Vortex sign-in can also stall the first load: open
								the sensor in a new tab, sign in, then retry here.
							</p>
							<div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
								<a
									href={SDD_URL}
									target="_blank"
									rel="noreferrer noopener"
									style={{ ...buttonStyle, textDecoration: "none" }}
								>
									Open in new tab
								</a>
								<button
									type="button"
									style={buttonStyle}
									onClick={this.handleRetry}
								>
									Retry
								</button>
								<button
									type="button"
									style={{ ...buttonStyle, opacity: 0.75 }}
									onClick={this.handleDismiss}
								>
									Keep waiting
								</button>
							</div>
						</div>
					</div>
				) : null}
			</div>
		);
	}
}
