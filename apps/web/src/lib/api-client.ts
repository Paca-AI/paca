import axios, {
	type AxiosInstance,
	type AxiosResponse,
	type InternalAxiosRequestConfig,
} from "axios";

import { ApiErrorCode } from "./api-error";

const API_BASE_URL = window.location.origin;

/**
 * SPA navigation callback injected after the router is created (see router.tsx).
 * Falls back to a hard redirect so it always works even before the router mounts.
 */
type NavigateFn = (to: string) => void;
let _navigate: NavigateFn = (to) => {
	window.location.href = to;
};
export function setNavigate(fn: NavigateFn) {
	_navigate = fn;
}

/**
 * Notified whenever a 401 causes the access_token cookie to actually be
 * refreshed. Consumers that hold a long-lived connection authenticated at
 * connect time (e.g. the realtime Socket.IO client, which reads the cookie
 * once and reuses it for the connection's whole lifetime) can use this to
 * re-authenticate against the fresh cookie instead of running on a stale one
 * until something else forces a reconnect.
 */
type TokenRefreshedListener = () => void;
const tokenRefreshedListeners = new Set<TokenRefreshedListener>();
export function onTokenRefreshed(listener: TokenRefreshedListener): () => void {
	tokenRefreshedListeners.add(listener);
	return () => tokenRefreshedListeners.delete(listener);
}

export class ApiClient {
	readonly instance: AxiosInstance;

	private isRefreshing = false;
	// The in-flight POST /auth/refresh, shared by every caller: the refresh
	// token rotates, so two concurrent refreshes would invalidate each other.
	private refreshPromise: Promise<void> | null = null;
	private refreshSubscribers: Array<{
		retry: () => void;
		reject: (err: unknown) => void;
	}> = [];

	constructor() {
		this.instance = axios.create({
			baseURL: `${API_BASE_URL}/api/v1`,
			withCredentials: true, // send HttpOnly cookies automatically
			headers: {
				"Content-Type": "application/json",
			},
		});

		this.instance.interceptors.request.use(
			(config: InternalAxiosRequestConfig) => config,
			(error) => Promise.reject(error),
		);

		this.instance.interceptors.response.use(
			(response: AxiosResponse) => response,
			async (error) => {
				const originalRequest = error.config as InternalAxiosRequestConfig & {
					_retry?: boolean;
				};

				// Redirect to the forced password-change page on this specific 403.
				// Guard: skip navigation when already on /change-password to prevent
				// re-entrant loops triggered by beforeLoad fetching /me.
				if (
					error.response?.status === 403 &&
					error.response?.data?.error_code ===
						ApiErrorCode.PasswordChangeRequired
				) {
					if (window.location.pathname !== "/change-password") {
						_navigate("/change-password");
					}
					return Promise.reject(error);
				}

				if (error.response?.status !== 401 || originalRequest._retry) {
					return Promise.reject(error);
				}

				// Skip refresh for auth endpoints to avoid infinite loops
				const url: string = originalRequest.url ?? "";
				const isAuthEndpoint =
					url.includes("/auth/login") ||
					// Second-step and pre-login MFA calls answer 401 for a wrong code or
					// password: that is the real result, with no session to refresh.
					url.includes("/auth/mfa/") ||
					url.includes("/auth/register") ||
					url.includes("/auth/refresh");

				if (isAuthEndpoint) {
					return Promise.reject(error);
				}

				if (this.isRefreshing) {
					// Queue the request until refresh completes
					return new Promise((resolve, reject) => {
						this.refreshSubscribers.push({
							retry: () => {
								originalRequest._retry = true;
								this.instance
									.request(originalRequest)
									.then(resolve)
									.catch(reject);
							},
							reject,
						});
					});
				}

				originalRequest._retry = true;
				this.isRefreshing = true;

				try {
					await this.refreshSession();
					tokenRefreshedListeners.forEach((listener) => {
						listener();
					});

					// Flush queued requests
					this.refreshSubscribers.forEach(({ retry }) => {
						retry();
					});
					this.refreshSubscribers = [];

					return this.instance.request(originalRequest);
				} catch (refreshError) {
					// Reject all queued requests so their promises settle instead of hanging
					this.refreshSubscribers.forEach(({ reject }) => {
						reject(refreshError);
					});
					this.refreshSubscribers = [];
					return Promise.reject(refreshError);
				} finally {
					this.isRefreshing = false;
				}
			},
		);
	}

	/**
	 * Refreshes the session cookie now, sharing one request with any refresh
	 * already in flight. Unlike the 401 path it does not notify
	 * `onTokenRefreshed` listeners: the caller (the realtime socket answering
	 * the server's "reauth") reconnects itself.
	 */
	refreshSession(): Promise<void> {
		this.refreshPromise ??= this.instance
			.post("/auth/refresh")
			.then(() => undefined)
			.finally(() => {
				this.refreshPromise = null;
			});
		return this.refreshPromise;
	}
}

export const apiClient = new ApiClient();
