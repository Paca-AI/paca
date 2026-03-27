import axios, {
	type AxiosInstance,
	type AxiosResponse,
	type InternalAxiosRequestConfig,
} from "axios";

const API_BASE_URL =
	import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080";

export class ApiClient {
	readonly instance: AxiosInstance;

	private isRefreshing = false;
	private refreshSubscribers: Array<() => void> = [];

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

				if (error.response?.status !== 401 || originalRequest._retry) {
					return Promise.reject(error);
				}

				// Skip refresh for auth endpoints to avoid infinite loops
				const url: string = originalRequest.url ?? "";
				const isAuthEndpoint =
					url.includes("/auth/login") ||
					url.includes("/auth/register") ||
					url.includes("/auth/refresh");

				if (isAuthEndpoint) {
					return Promise.reject(error);
				}

				if (this.isRefreshing) {
					// Queue the request until refresh completes
					return new Promise((resolve, reject) => {
						this.refreshSubscribers.push(() => {
							originalRequest._retry = true;
							this.instance
								.request(originalRequest)
								.then(resolve)
								.catch(reject);
						});
					});
				}

				originalRequest._retry = true;
				this.isRefreshing = true;

				try {
					await this.instance.post("/auth/refresh");

					// Flush queued requests
					this.refreshSubscribers.forEach((cb) => {
						cb();
					});
					this.refreshSubscribers = [];

					return this.instance.request(originalRequest);
				} catch (refreshError) {
					this.refreshSubscribers = [];
					return Promise.reject(refreshError);
				} finally {
					this.isRefreshing = false;
				}
			},
		);
	}
}

export const apiClient = new ApiClient();
