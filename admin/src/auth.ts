import { createContext } from "preact";
import { useContext } from "preact/hooks";
import type { User } from "./api";

export type AuthValue = { user: User; logout: () => void };

const AuthContext = createContext<AuthValue>(null as unknown as AuthValue);

export const AuthProvider = AuthContext.Provider;
export const useAuth = () => useContext(AuthContext);
