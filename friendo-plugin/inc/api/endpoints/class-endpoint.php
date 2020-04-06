<?php

namespace Friendo\Api\Endpoints;

use WP_REST_Request;

abstract class Endpoint {

    abstract protected function getRoute();

    abstract protected function getMethods();

    abstract protected function handleRequest(WP_REST_Request $request);

    abstract protected function permissionCallback();

    abstract protected function getArgs();

    final public function registerRoute () {
        register_rest_route( 'friendo/v1', "/{$this->getRoute()}/", [
            'methods' => [$this, 'getMethods'],
            'callback' => [$this, 'handleRequest'],
            'args' => [$this, 'getArgs'],
            'permission_callback' => [$this, 'permissionCallback']
        ] );
    }
}