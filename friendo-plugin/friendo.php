<?php
/**
 * Plugin Name: Friendo Plugin
 */

namespace Friendo;

defined( 'ABSPATH' ) || exit;

// Define constants
if ( ! defined( 'FRIENDO_PLUGIN_PATH' ) ) {
define( 'FRIENDO_PLUGIN_PATH', plugin_dir_path( __FILE__ ) );
}

if ( ! defined( 'FRIENDO_PLUGIN_URI' ) ) {
    define( 'FRIENDO_PLUGIN_URI', plugin_dir_url( __FILE__ ) );
}

if ( ! class_exists( 'Friendo' ) ) {
    require_once FRIENDO_PLUGIN_PATH . 'inc/class-friendo.php';
    Friendo::init();
}

?>