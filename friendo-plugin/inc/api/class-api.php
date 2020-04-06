<?php

namespace Friendo;

class Api
{

    public function init ()
    {
        $api = new self;
        $api->loadApi();
    }

    protected function loadApi() {
        add_action( 'rest_api_init', [$this, 'registerRoute']);
    }

    protected function registerRoute () {
        register_rest_route( 'friendo/v1', '/author/(?P<id>\d+)', array(
            'methods' => 'GET',
            'callback' => 'handleRequest',
        ) );
    }

    protected function handleReqest () {

    }

    protected function 

}