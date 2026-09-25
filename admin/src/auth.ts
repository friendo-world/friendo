import { createContext } from "preact";
import { useContext } from "preact/hooks";
import type { Features, User } from "./api";

// Who is signed in, plus which of the site's features are on (so a screen for a
// feature that's off can step aside). `reloadFeatures` is for Settings, after a
// switch is flipped.
export type AuthValue = { user: User; logout: () => void; features: Features; reloadFeatures: () => void };

const AuthContext = createContext<AuthValue>(null as unknown as AuthValue);

export const AuthProvider = AuthContext.Provider;
export const useAuth = () => useContext(AuthContext);
