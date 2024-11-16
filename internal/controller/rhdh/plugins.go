package rhdh

type Plugin struct {
	Package   string
	Integrity string
}

const Orchestrator string = "orchestrator"
const OrchestratorBackend string = "orchestratorBackend"
const Notification string = "notifications"
const NotificationBackend string = "notificationsBackend"
const Signals string = "signals"
const SignalsBackend string = "signalsBackend"
const NotificationsEmail string = "notificationsEmail"

func getPlugins() map[string]Plugin {
	return map[string]Plugin{
		Orchestrator: {
			Package:   "backstage-plugin-orchestrator@1.3.0-rc.3",
			Integrity: "sha512-s8SeUMLr9L9oqc2EHqn+KxQlFqXD/OIr3hS/jVUWhxfnC7cwfFNiqZG1c5Kl9vtI16zAc8MUf+qhsCd7S1MYvg==",
		},
		OrchestratorBackend: {
			Package:   "backstage-plugin-orchestrator-backend-dynamic@1.3.0-rc.3",
			Integrity: "sha512-08cllbcquVA6QLuO0XknxdynS5mvAazb0s9zES1AkuFn2GR7ZKIuIZMjcUwVjHEthwv4UdSNPB7W3IFDsmSDZw==",
		},
		Notification: {
			Package:   "plugin-notifications-dynamic@1.3.0-rc.3",
			Integrity: "sha512-zqwK318o+Lc16pV5wvN6IWMLFqImOWr0xbsGBI69YNVGpXA6AOccXInGbn1RA1QKXfV5sNo8xc5N0WIIgx43Iw==",
		},
		NotificationBackend: {
			Package:   "plugin-notifications-backend-dynamic@1.3.0-rc.3",
			Integrity: "sha512-2qai8t66dyHEIaPFjdJ9M5nPh53vkH5O7Keed/lFNH0TbPoxamql9V0tdOwdx5Mb7bJwj9N1ulin/mCNniFuTA==",
		},
		Signals: {
			Package:   "plugin-signals-dynamic@1.3.0-rc.3",
			Integrity: "sha512-WRUi5xpJDD5Jd2p+juCIpsXCnXfHLoSwPZ/N7a7ZnqarfajTkL8qOglhIJh+lVTbe65S8v1rtQLGj9bTCXuPlA==",
		},
		SignalsBackend: {
			Package:   "plugin-signals-backend-dynamic@1.3.0-rc.3",
			Integrity: "sha512-FgmPouKc2FuHSMfmkdXCVx0/1kPlT6OVbRUNFzOJGSjZAj0nvxSg+W3pt15dSOC5Fe5j2FLSuevCx34YVA+VzQ==",
		},
		NotificationsEmail: {
			Package:   "plugin-notifications-backend-module-email-dynamic@1.3.0-rc.3",
			Integrity: "sha512-uIGPDdSha9H1kWwofYJXg/GgrGZuF9WZTXgRb8YtN4iKAAZ9FLAD9BuLobUKYXbzO6jGaNzIw82kTJa1VhvEzg==",
		},
	}

}
