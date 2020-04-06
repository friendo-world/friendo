import { createContext, useContext, useReducer } from 'react';
import initialState from './initialState';

export const StoreContext = createContext();

export const StoreProvider = ( { reducer, initialState, children } ) =>(
	<StoreContext.Provider value={ useReducer( reducer, initialState ) }>
		{ children }
	</StoreContext.Provider>
);

export const useStoreValue = () => useContext( StoreContext );