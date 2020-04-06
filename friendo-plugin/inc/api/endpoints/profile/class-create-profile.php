<?php

namespace Friendo\Api\Endpoints;

require_once FRIENDO_PLUGIN_PATH . 'inc/api/endpoints/class-endpoint.php';
require_once FRIENDO_PLUGIN_PATH . 'inc/api/endpoints/class-utils.php';

use WP_REST_Server;
use Friendo\Api\Endpoints\Utils;

class CreateProfileEndpoint extends Endpoint {

    protected function getRoute ()
    {
        return 'profile';
    }

    protected function getMethods ()
    {
        return WP_REST_Server::CREATABLE;
    }

    protected function handleRequest ()
    {
        
    }

    protected function permissionCallback ()
    {
        $utils = new Utils();
        return $utils->canEdit();
    }

    protected function getArgs ()
    {
        return [
            'handle' => [
                'validate_callback' => 'is_numeric'
            ],
        ];
    }

}