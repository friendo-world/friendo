export const reducer = ( state, action ) => {
	switch ( action.type ) {
		case 'SET_MENU':
			console.log('set menu!!')
			return {
				...state,
				activeMenu: action.payload.menu,
			};
		default:
			return state;
	}
};