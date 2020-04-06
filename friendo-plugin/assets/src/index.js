// Entry point for Friendo app

// Vendor dependencies
import React from 'react';
import ReactDOM from 'react-dom';
window.React = React;

// App component
import App from './components/app/index.js';

import { StoreProvider } from './store';
import { reducer } from './store/reducer';
import initialState from './store/initialState';

ReactDOM.render(
	<StoreProvider initialState={ initialState } reducer={ reducer }>
		<App/>
	</StoreProvider>,
    document.getElementById('app')
);